package learning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lengzhao/agentkit"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

// ReviewConfig controls the background review LLM loop.
type ReviewConfig struct {
	MaxSteps          int
	MaxDigestMessages int
	// SessionRecall is optional FTS hits from prior sessions (appended to the digest).
	SessionRecall string
}

func (c ReviewConfig) normalized() ReviewConfig {
	if c.MaxSteps <= 0 {
		c.MaxSteps = 10
	}
	if c.MaxDigestMessages <= 0 {
		c.MaxDigestMessages = 24
	}
	return c
}

const reviewSystemPrompt = `You are a background memory and skill curator for an AI assistant.
You review a completed conversation turn and decide whether anything durable should be saved.

Rules:
- Save only stable preferences, user-specific facts, environment conventions, or reusable workflows.
- Skip trivia, one-off debugging, secrets, and anything easy to look up again.
- Use learn_capture with action memory_add for compact personal memory (one fact per call); all durable facts go to memory.md.
- Use learn_capture with action memory_remove only to fix wrong memory (old_text substring).
- Use learn_capture with action skill_propose only for non-trivial reusable procedures (not single commands).
- If nothing is worth saving, reply with a short summary and do not call tools.`

// ReviewResult summarizes one background review run.
type ReviewResult struct {
	Summary      string
	Steps        int
	InputTokens  int
	OutputTokens int
	Cancelled    bool
}

// RunReview executes a isolated tool loop against the conversation digest.
func RunReview(
	ctx context.Context,
	cfg ReviewConfig,
	llm agentkit.LLMProvider,
	tools agentkit.ToolRuntime,
	model string,
	transcript []agentkit.ModelMessage,
) (*ReviewResult, error) {
	cfg = cfg.normalized()
	if llm == nil {
		return nil, fmt.Errorf("review requires llm")
	}
	if tools == nil {
		return nil, fmt.Errorf("review requires tools")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("review requires model")
	}
	digest := DigestMessages(transcript, cfg.MaxDigestMessages)
	if strings.TrimSpace(digest) == "" {
		return &ReviewResult{}, nil
	}

	specs, err := tools.Visible(ctx)
	if err != nil {
		return nil, err
	}

	userPrompt := digest
	if recall := strings.TrimSpace(cfg.SessionRecall); recall != "" {
		userPrompt += "\n\n---\nPrior session search (same tenant):\n" + recall
	}
	messages := []agentkit.ModelMessage{
		{Role: "system", Content: []agentkit.ContentPart{{Type: "text", Text: reviewSystemPrompt}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: userPrompt}}},
	}

	ctx, endObservation := telemetry.BeginObservation(ctx, telemetry.ObservationMetaFromContext(ctx, captelemetry.ObservationMeta{
		Name:  "learning.review",
		Kind:  captelemetry.KindSpan,
		Model: model,
	}))
	var obsEnd captelemetry.ObservationEnd
	defer func() {
		endObservation(obsEnd)
	}()

	res := &ReviewResult{}
	var lastText string
	for step := 0; step < cfg.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			res.Summary = lastText
			res.Steps = step
			res.Cancelled = true
			obsEnd.Err = err
			return res, err
		}
		assistant, usage, err := completeOnce(ctx, llm, model, messages, specs)
		if err != nil {
			res.Summary = lastText
			res.Steps = step
			obsEnd.Err = err
			return res, err
		}
		if usage != nil {
			res.InputTokens += usage.InputTokens
			res.OutputTokens += usage.OutputTokens
		}
		lastText = flattenAssistantText(assistant)
		messages = append(messages, assistant)
		res.Steps = step + 1
		if len(assistant.ToolCalls) == 0 {
			res.Summary = lastText
			obsEnd.Output = lastText
			return res, nil
		}
		var results []agentkit.ToolResult
		for _, call := range assistant.ToolCalls {
			result, err := tools.Execute(ctx, call)
			if err != nil {
				result = agentkit.ResultFromCall(call, "error: "+err.Error())
			}
			results = append(results, result)
		}
		messages = append(messages, agentkit.ModelMessage{Role: "tool", ToolResults: results})
	}
	res.Summary = lastText
	err = fmt.Errorf("review exceeded %d steps", cfg.MaxSteps)
	obsEnd.Err = err
	return res, err
}

func completeOnce(
	ctx context.Context,
	llm agentkit.LLMProvider,
	model string,
	messages []agentkit.ModelMessage,
	specs []agentkit.ToolSpec,
) (agentkit.ModelMessage, *agentkit.Usage, error) {
	stream, err := llm.Stream(ctx, agentkit.LLMRequest{
		Model:    model,
		Messages: messages,
		Tools:    specs,
	})
	if err != nil {
		return agentkit.ModelMessage{}, nil, err
	}
	defer stream.Close()

	var assistant agentkit.ModelMessage
	var usage *agentkit.Usage
	for {
		ev, err := stream.Recv()
		if ev.Message != nil {
			assistant = *ev.Message
		}
		if ev.Usage != nil {
			usage = ev.Usage
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return agentkit.ModelMessage{}, usage, err
		}
	}
	if assistant.Role == "" {
		assistant.Role = "assistant"
	}
	return assistant, usage, nil
}

// DigestMessages formats recent model-visible messages for the review prompt.
func DigestMessages(messages []agentkit.ModelMessage, limit int) string {
	if limit <= 0 {
		limit = 24
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	var b strings.Builder
	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			continue
		}
		text := flattenMessageParts(msg.Content)
		if role == "tool" {
			for _, tr := range msg.ToolResults {
				line := strings.TrimSpace(tr.Content)
				if line == "" {
					continue
				}
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString("tool ")
				b.WriteString(tr.Name)
				b.WriteString(": ")
				b.WriteString(truncateLine(line, 800))
			}
			continue
		}
		if role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, call := range msg.ToolCalls {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString("assistant tool_call ")
				b.WriteString(call.Name)
			}
		}
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(truncateLine(text, 1200))
	}
	return b.String()
}

func flattenMessageParts(parts []agentkit.ContentPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type != "text" {
			continue
		}
		t := strings.TrimSpace(p.Text)
		if t == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(t)
	}
	return b.String()
}

func flattenAssistantText(msg agentkit.ModelMessage) string {
	return flattenMessageParts(msg.Content)
}

func truncateLine(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// CaptureInput is the learn_capture tool schema.
type CaptureInput struct {
	Action  string `json:"action" jsonschema:"memory_add | memory_remove | skill_propose"`
	Content string `json:"content,omitempty" jsonschema:"Text for memory_add or skill body for skill_propose"`
	OldText string `json:"old_text,omitempty" jsonschema:"Substring for memory_remove"`
	Name    string `json:"name,omitempty" jsonschema:"Skill name for skill_propose"`
	Focus   string `json:"focus,omitempty" jsonschema:"Short focus for skill_propose"`
}

// CaptureOutput is returned to the review model.
type CaptureOutput struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// ApplyCapture applies one learn_capture action via the learning service API.
type CaptureApplier interface {
	CaptureMemoryAdd(ctx context.Context, text, source string) (string, error)
	CaptureMemoryRemove(ctx context.Context, oldText string) (string, error)
	CaptureSkillPropose(ctx context.Context, name, body, sessionID, focus, source string) (string, error)
}

func ApplyCapture(ctx context.Context, app CaptureApplier, sessionID string, in CaptureInput) (CaptureOutput, error) {
	if app == nil {
		return CaptureOutput{}, fmt.Errorf("capture applier is required")
	}
	action := strings.ToLower(strings.TrimSpace(in.Action))
	switch action {
	case "memory_add":
		text := strings.TrimSpace(in.Content)
		if text == "" {
			return CaptureOutput{}, fmt.Errorf("content is required for memory_add")
		}
		msg, err := app.CaptureMemoryAdd(ctx, text, "background-review")
		if err != nil {
			return CaptureOutput{OK: false, Message: err.Error()}, nil
		}
		return CaptureOutput{OK: true, Message: msg}, nil
	case "memory_remove":
		old := strings.TrimSpace(in.OldText)
		if old == "" {
			return CaptureOutput{}, fmt.Errorf("old_text is required for memory_remove")
		}
		msg, err := app.CaptureMemoryRemove(ctx, old)
		if err != nil {
			return CaptureOutput{OK: false, Message: err.Error()}, nil
		}
		return CaptureOutput{OK: true, Message: msg}, nil
	case "skill_propose":
		body := strings.TrimSpace(in.Content)
		if body == "" {
			return CaptureOutput{}, fmt.Errorf("content is required for skill_propose")
		}
		name := strings.TrimSpace(in.Name)
		focus := strings.TrimSpace(in.Focus)
		msg, err := app.CaptureSkillPropose(ctx, name, body, sessionID, focus, "background-review")
		if err != nil {
			return CaptureOutput{OK: false, Message: err.Error()}, nil
		}
		return CaptureOutput{OK: true, Message: msg}, nil
	default:
		return CaptureOutput{}, fmt.Errorf("unknown action %q", in.Action)
	}
}

// FormatCaptureResult JSON-encodes CaptureOutput for Tool.Call.
func FormatCaptureResult(out CaptureOutput) (string, error) {
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
