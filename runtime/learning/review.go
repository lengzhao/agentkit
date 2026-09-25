package learning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

// ReviewConfig controls the background review LLM loop.
type ReviewConfig struct {
	MaxSteps          int
	MaxDigestMessages int
	// SessionRecall is optional FTS hits from prior sessions (appended to the digest).
	SessionRecall string
	// SignalCandidates is optional grounded dreaming signals for consolidation.
	SignalCandidates string
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
- Use learn_capture with action memory_replace to update one entry (old_text unique substring, content is the new text).
- Use learn_capture with action memory_remove only to fix wrong memory (old_text substring).
- Use learn_capture with action skill_propose only for non-trivial reusable procedures (not single commands).
- When a "Grounded memory candidates" section is present, promote only facts that are durable and not already covered in memory.md.
- If nothing is worth saving, reply with a short summary and do not call tools.`

// ReviewResult summarizes one background review run.
type ReviewResult struct {
	Summary      string
	Steps        int
	InputTokens  int
	OutputTokens int
	Cancelled    bool
	// Notices are user-visible lines from successful learn_capture memory/skill actions.
	Notices []string
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
	if candidates := strings.TrimSpace(cfg.SignalCandidates); candidates != "" {
		userPrompt += "\n\n---\n" + candidates
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
			result, err = agentkit.RecoverToolExecute(call, result, err)
			if err != nil {
				res.Summary = lastText
				res.Steps = step
				obsEnd.Err = err
				return res, err
			}
			if call.Name == "learn_capture" {
				appendCaptureNotice(res, result.Content)
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
		if err := ctx.Err(); err != nil {
			return agentkit.ModelMessage{}, usage, err
		}
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
		text := capsession.FlattenTextParts(msg.Content, "\n")
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
				b.WriteString(TruncateEllipsis(line, 800))
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
		b.WriteString(TruncateEllipsis(text, 1200))
	}
	return b.String()
}

func flattenAssistantText(msg agentkit.ModelMessage) string {
	return capsession.FlattenTextParts(msg.Content, "\n")
}

func appendCaptureNotice(res *ReviewResult, raw string) {
	if res == nil {
		return
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	var out CaptureOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return
	}
	if !out.OK || strings.TrimSpace(out.Message) == "" {
		return
	}
	res.Notices = append(res.Notices, out.Message)
}
