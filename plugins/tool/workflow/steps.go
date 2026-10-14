package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
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
	// Prefer raw structured execution (deps.shell) so we don't parse bash text.
	if e.Shell != nil {
		return e.runScriptStepStructured(ctx, wf, runID, step, shellCmd, state)
	}
	if e.Tools == nil {
		return fmt.Errorf("script step requires tools runtime (bash) or deps.shell")
	}
	input := map[string]any{"command": shellCmd}
	if step.TimeoutSeconds > 0 {
		input["timeout"] = float64(step.TimeoutSeconds)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode bash input: %w", err)
	}
	call := agentkit.ToolCall{
		ID:    agentkit.ToolCallID("wf/" + wf.Name + "/" + step.ID + "/bash"),
		Name:  bashToolName,
		Input: raw,
	}
	result, err := e.Tools.Execute(ctx, call)
	e.emitStepToolEvents(ctx, call, result)
	if err != nil {
		return err
	}
	if bashErr := bashStepError(result.Content); bashErr != "" {
		return fmt.Errorf("%s", bashErr)
	}
	state.json = parseJSONContent(result.Content)
	state.view, state.path = e.spillView(ctx, runID, step.ID, ".out", result.Content)
	if spill := bashSpillPath(result.Content); spill != "" && state.path == "" {
		state.path = spill
	}
	return nil
}

// runScriptStepStructured runs the command via deps.shell (StructuredShell)
// and treats non-zero exit / timeout / cancel as step failure. Combined
// output is kept verbatim; workflow spillView caps the inline view.
// Emits a workflow.script span so script execution is observable like tool steps.
func (e *Engine) runScriptStepStructured(ctx context.Context, wf *Workflow, runID string, step *Step, shellCmd string, state *stepState) error {
	var timeoutSec *float64
	if step.TimeoutSeconds > 0 {
		s := float64(step.TimeoutSeconds)
		timeoutSec = &s
	}
	scriptInput, _ := json.Marshal(map[string]any{"command": shellCmd})
	if timeoutSec != nil {
		var m map[string]any
		if json.Unmarshal(scriptInput, &m) == nil {
			m["timeout"] = *timeoutSec
			scriptInput, _ = json.Marshal(m)
		}
	}
	scriptCtx, endScript := e.beginScriptObservation(ctx, wf, runID, step.ID, scriptInput)
	stdout, stderr, exitCode, err := e.Shell.RunStructured(scriptCtx, shellCmd, timeoutSec)
	// Emit as if the step called a tool so platforms show script execution in the stream.
	scriptCall := agentkit.ToolCall{
		ID:    agentkit.ToolCallID("wf/" + wf.Name + "/" + step.ID + "/script"),
		Name:  "script",
		Input: scriptInput,
	}
	scriptContent := stdout
	if strings.TrimSpace(stderr) != "" {
		scriptContent = strings.TrimRight(scriptContent, "\n") + "\n[stderr]\n" + stderr
	}
	if err != nil {
		scriptContent = scriptContent + "\n" + err.Error()
	}
	scriptResult := agentkit.ToolResult{
		ID:      scriptCall.ID,
		Name:    "script",
		Content: scriptContent,
	}
	e.emitStepToolEvents(ctx, scriptCall, scriptResult)
	scriptErr := ""
	if err != nil {
		scriptErr = err.Error()
	} else if exitCode != 0 {
		scriptErr = fmt.Sprintf("exit code %d", exitCode)
	}
	if endScript != nil {
		var obsErr error
		if scriptErr != "" {
			obsErr = fmt.Errorf("%s", scriptErr)
		}
		endScript(captelemetry.ObservationEnd{Output: scriptContent, Err: obsErr})
	}
	if err != nil {
		return fmt.Errorf("script step %q: %w", step.ID, err)
	}
	combined := stdout
	if strings.TrimSpace(stderr) != "" {
		combined = strings.TrimRight(combined, "\n") + "\n[stderr]\n" + stderr
	}
	state.json = parseJSONContent(stdout)
	state.view, state.path = e.spillView(ctx, runID, step.ID, ".out", combined)
	if exitCode != 0 {
		return fmt.Errorf("script step %q: exit code %d: %s", step.ID, exitCode, truncateRunes(stderr+stdout, 400))
	}
	return nil
}

// beginScriptObservation starts a workflow.script span (input = command,
// output = combined stdout/stderr) so script execution is observable like
// tool steps even though it bypasses the tools runtime.
func (e *Engine) beginScriptObservation(ctx context.Context, wf *Workflow, runID, stepID string, input []byte) (context.Context, func(captelemetry.ObservationEnd)) {
	if e.Telemetry == nil {
		return ctx, nil
	}
	return e.Telemetry.BeginObservation(ctx, e.Telemetry.ObservationMetaFromContext(ctx, captelemetry.ObservationMeta{
		Name:  "workflow.script",
		Kind:  captelemetry.KindSpan,
		Input: truncateRunes(string(input), 1024),
		Attributes: map[string]string{
			"workflow": wf.Name,
			"run_id":   runID,
			"step":     stepID,
		},
	}))
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
	call := agentkit.ToolCall{
		ID:    agentkit.ToolCallID("wf/" + wf.Name + "/" + step.ID),
		Name:  step.Tool,
		Input: rawArgs,
	}
	result, err := e.Tools.Execute(ctx, call)
	e.emitStepToolEvents(ctx, call, result)
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
	genCtx, endGen := e.beginModelGeneration(ctx, wf, step, messages)
	text, err := rtllm.CompleteText(genCtx, e.LLM, agentkit.LLMRequest{Model: step.Model, Messages: messages})
	if endGen != nil {
		endGen(captelemetry.ObservationEnd{Output: text, Err: err})
	}
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

// beginModelGeneration opens an llm.generation observation for a model step
// so the LLM call is visible (prompt/completion) under the workflow.step span,
// mirroring how the agent loop records generations.
func (e *Engine) beginModelGeneration(ctx context.Context, wf *Workflow, step *Step, messages []agentkit.ModelMessage) (context.Context, func(captelemetry.ObservationEnd)) {
	if e.Telemetry == nil {
		return ctx, nil
	}
	return e.Telemetry.BeginObservation(ctx, e.Telemetry.ObservationMetaFromContext(ctx, captelemetry.ObservationMeta{
		Name:               "llm.generation",
		Kind:               captelemetry.KindGeneration,
		Model:              step.Model,
		GenerationMessages: messages,
		Attributes: map[string]string{
			"workflow": wf.Name,
			"step":     step.ID,
		},
	}))
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
