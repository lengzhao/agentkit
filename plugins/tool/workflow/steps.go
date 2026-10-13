package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	rtllm "github.com/lengzhao/agentkit/runtime/llm"
)

// bashToolName is the model-visible name of tool/shell-bash; script steps delegate to it.
const bashToolName = "bash"

// DefaultModelMaxInputBytes bounds the rendered model-step prompt. Exceeding
// it fails the step instead of silently truncating: routing a large payload
// back through a model is exactly what workflows exist to avoid.
const DefaultModelMaxInputBytes = 16384

const modelJSONInstruction = "Respond with a single JSON value that validates against this JSON Schema. " +
	"No markdown fence, no commentary, JSON only.\nSchema:\n"

func (e *Engine) runScriptStep(ctx context.Context, wf *Workflow, runID string, step *Step, data map[string]any, state *stepState) error {
	if e.Tools == nil {
		return fmt.Errorf("script step requires tools runtime (bash)")
	}
	command, err := renderTemplate(wf.Name+"."+step.ID+".run", step.Run, data)
	if err != nil {
		return err
	}
	dir := wf.Dir
	if strings.TrimSpace(step.Dir) != "" {
		rendered, err := renderTemplate(wf.Name+"."+step.ID+".dir", step.Dir, data)
		if err != nil {
			return err
		}
		if filepath.IsAbs(rendered) {
			dir = rendered
		} else {
			dir = filepath.Join(wf.Dir, rendered)
		}
	}
	shellCmd := command
	if dir != "" {
		shellCmd = fmt.Sprintf("cd %q && %s", dir, command)
	}
	input := map[string]any{"command": shellCmd}
	if step.TimeoutSeconds > 0 {
		input["timeout"] = float64(step.TimeoutSeconds)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode bash input: %w", err)
	}
	result, err := e.Tools.Execute(ctx, agentkit.ToolCall{
		ID:    agentkit.ToolCallID("wf/" + wf.Name + "/" + step.ID + "/bash"),
		Name:  bashToolName,
		Input: raw,
	})
	if err != nil {
		return err
	}
	if bashErr := bashStepError(result.Content); bashErr != "" {
		return fmt.Errorf("%s", bashErr)
	}
	state.view, state.path = e.spillView(ctx, runID, step.ID, ".out", result.Content)
	if spill := bashSpillPath(result.Content); spill != "" && state.path == "" {
		state.path = spill
	}
	return nil
}

// bashStepError maps tool/shell-bash model text back to a step failure.
func bashStepError(content string) string {
	if strings.Contains(content, "\n\nCommand exited with code") {
		return truncateRunes(content, 400)
	}
	if strings.Contains(content, "Command timed out after") {
		return truncateRunes(content, 400)
	}
	if strings.Contains(content, "\n\nCommand aborted") {
		return truncateRunes(content, 400)
	}
	return ""
}

func bashSpillPath(content string) string {
	const marker = "Full output: "
	idx := strings.LastIndex(content, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(content[idx+len(marker):])
}

func (e *Engine) runToolStep(ctx context.Context, wf *Workflow, runID string, step *Step, data map[string]any, state *stepState) error {
	if e.Tools == nil {
		return fmt.Errorf("tool step requires a tools runtime dependency")
	}
	args, err := renderArgs(step.Args, data)
	if err != nil {
		return err
	}
	rawArgs, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("encode tool args: %w", err)
	}
	result, err := e.Tools.Execute(ctx, agentkit.ToolCall{
		ID:    agentkit.ToolCallID("wf/" + wf.Name + "/" + step.ID),
		Name:  step.Tool,
		Input: rawArgs,
	})
	if err != nil {
		return err
	}
	state.json = parseJSONContent(result.Content)
	state.view, state.path = e.spillView(ctx, runID, step.ID, ".out", result.Content)
	return nil
}

func (e *Engine) runModelStep(ctx context.Context, wf *Workflow, step *Step, data map[string]any, state *stepState) error {
	if e.LLM == nil {
		return fmt.Errorf("model step requires an llm dependency")
	}
	prompt, err := renderTemplate(wf.Name+"."+step.ID+".prompt", step.Prompt, data)
	if err != nil {
		return err
	}
	maxBytes := step.MaxInputBytes
	if maxBytes <= 0 {
		maxBytes = DefaultModelMaxInputBytes
	}
	if len(prompt) > maxBytes {
		return fmt.Errorf("rendered prompt is %d bytes, exceeds maxInputBytes %d; distill the data in an earlier step instead of enlarging the limit", len(prompt), maxBytes)
	}
	messages := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: prompt}}},
	}
	if len(step.OutputSchema) > 0 {
		rawSchema, err := json.Marshal(step.OutputSchema)
		if err != nil {
			return fmt.Errorf("encode outputSchema: %w", err)
		}
		messages = append([]agentkit.ModelMessage{
			{Role: "system", Content: []agentkit.ContentPart{{Type: "text", Text: modelJSONInstruction + string(rawSchema)}}},
		}, messages...)
	}
	text, err := rtllm.CompleteText(ctx, e.LLM, agentkit.LLMRequest{Model: step.Model, Messages: messages})
	if err != nil {
		return err
	}
	state.view = text
	if len(step.OutputSchema) > 0 {
		parsed, err := parseModelJSON(text)
		if err != nil {
			return err
		}
		if err := validateAgainstSchema(step.OutputSchema, parsed); err != nil {
			return fmt.Errorf("model output does not match outputSchema: %w", err)
		}
		state.json = parsed
	}
	return nil
}

// parseModelJSON extracts the first JSON value from model text, tolerating a
// markdown fence around it.
func parseModelJSON(text string) (any, error) {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") {
		if i := strings.Index(trimmed, "\n"); i >= 0 {
			trimmed = trimmed[i+1:]
		}
		trimmed = strings.TrimSuffix(strings.TrimSpace(trimmed), "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return nil, fmt.Errorf("model did not return valid JSON: %w", err)
	}
	return value, nil
}

func (e *Engine) runAgentStep(ctx context.Context, wf *Workflow, step *Step, data map[string]any, state *stepState) error {
	if e.Subagent == nil {
		return fmt.Errorf("agent step requires a subagent dependency")
	}
	task, err := renderTemplate(wf.Name+"."+step.ID+".task", step.Task, data)
	if err != nil {
		return err
	}
	async := false // pipelines wait for the conclusion; follow-up delivery is not pipeline semantics
	var timeout *int
	if step.TimeoutSeconds > 0 {
		timeout = &step.TimeoutSeconds
	}
	result, err := e.Subagent.Run(ctx, subagent.Request{
		Agent:          step.Agent,
		Task:           task,
		Async:          &async,
		TimeoutSeconds: timeout,
	})
	if err != nil {
		return err
	}
	state.view = result.Summary
	state.session = result.Session
	state.json = map[string]any{
		"status":  result.Status,
		"steps":   result.Steps,
		"session": result.Session,
	}
	return nil
}
