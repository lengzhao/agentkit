package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
)

// MaxViewBytes caps the inline view a step state exposes to templates; larger
// outputs are written to an artifact file and referenced by path instead.
const MaxViewBytes = 50 * 1024

// Engine runs workflows against injected capabilities. All fields except
// Workspace are optional per step type; a step whose capability is missing
// fails with a clear error.
type Engine struct {
	// Tools executes tool steps through the policy/approval pipeline. Use a
	// sibling tools/runtime instance that does not include tool/workflow.
	Tools agentkit.ToolRuntime
	// Workspace resolves scoped dirs and the artifact root.
	Workspace workspace.Service
	// LLM backs model steps (one-shot completion).
	LLM agentkit.LLMProvider
	// Subagent backs agent steps.
	Subagent subagent.Spawner
	// Telemetry receives workflow.step / workflow.run events.
	Telemetry captelemetry.Toolkit
}

// stepState is one finished step's template-visible outcome.
type stepState struct {
	status  string
	view    string // truncated inline view (stdout / text / summary)
	path    string // artifact file with the full output, when spilled
	json    any    // parsed structured output (tool JSON result, model outputSchema)
	session string // agent step child session id
	err     string
}

// JSON wraps a structured step output so direct template actions
// ({{ .steps.x.json }}) render compact JSON while dig can still navigate it.
type JSON struct{ V any }

func (j JSON) String() string {
	raw, err := json.Marshal(j.V)
	if err != nil {
		return "null"
	}
	return string(raw)
}

func (s *stepState) templateData() map[string]any {
	var jsonValue any = ""
	if s.json != nil {
		jsonValue = JSON{V: s.json}
	}
	return map[string]any{
		"status":  s.status,
		"stdout":  s.view,
		"text":    s.view,
		"path":    s.path,
		"json":    jsonValue,
		"session": s.session,
		"error":   s.err,
	}
}

// Run executes the workflow serially and returns the rendered result template.
func (e *Engine) Run(ctx context.Context, wf *Workflow, args map[string]any) (string, error) {
	if e.Workspace == nil {
		return "", fmt.Errorf("workflow engine requires workspace")
	}
	if args == nil {
		args = map[string]any{}
	}
	if err := validateInputs(wf, args); err != nil {
		return "", err
	}
	runID := time.Now().UTC().Format("20060102T150405") + "-" + uuid.NewString()[:8]
	runDir, err := e.artifactDir(ctx, runID)
	if err != nil {
		return "", err
	}
	states := make(map[string]*stepState, len(wf.Steps))
	data := map[string]any{
		"inputs": args,
		"steps":  map[string]any{},
		"workflow": map[string]any{
			"name":   wf.Name,
			"dir":    wf.Dir,
			"runDir": runDir,
		},
	}
	stepsData := data["steps"].(map[string]any)

	var failed error
	for i := range wf.Steps {
		step := &wf.Steps[i]
		state := e.runStep(ctx, wf, runID, step, data)
		states[step.ID] = state
		stepsData[step.ID] = state.templateData()
		if state.err != "" && !step.ContinueOnError {
			failed = fmt.Errorf("workflow %q step %q: %s", wf.Name, step.ID, state.err)
			break
		}
	}
	e.recordEvent(ctx, "workflow.run", map[string]string{
		"workflow": wf.Name,
		"run_id":   runID,
		"status":   runStatus(failed),
	})
	if failed != nil {
		return "", failed
	}
	result, err := renderTemplate(wf.Name+".result", wf.Result, data)
	if err != nil {
		return "", err
	}
	return result, nil
}

func runStatus(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}

func (e *Engine) runStep(ctx context.Context, wf *Workflow, runID string, step *Step, data map[string]any) *stepState {
	started := time.Now()
	state := &stepState{status: "ok"}
	stepCtx := ctx
	cancel := func() {}
	if step.TimeoutSeconds > 0 {
		stepCtx, cancel = context.WithTimeout(ctx, time.Duration(step.TimeoutSeconds)*time.Second)
	}
	defer cancel()

	var err error
	switch step.Type {
	case StepScript:
		err = e.runScriptStep(stepCtx, wf, runID, step, data, state)
	case StepTool:
		err = e.runToolStep(stepCtx, wf, runID, step, data, state)
	case StepModel:
		err = e.runModelStep(stepCtx, wf, step, data, state)
	case StepAgent:
		err = e.runAgentStep(stepCtx, wf, step, data, state)
	default:
		err = fmt.Errorf("unknown step type %q", step.Type)
	}
	if err != nil {
		state.status = "error"
		state.err = err.Error()
		slog.Warn("workflow step failed", "workflow", wf.Name, "step", step.ID, "type", step.Type, "error", err)
	}
	attrs := map[string]string{
		"workflow":    wf.Name,
		"run_id":      runID,
		"step":        step.ID,
		"type":        step.Type,
		"status":      state.status,
		"duration_ms": fmt.Sprint(time.Since(started).Milliseconds()),
	}
	if state.err != "" {
		attrs["error"] = truncateRunes(state.err, 200)
	}
	e.recordEvent(ctx, "workflow.step", attrs)
	return state
}

func (e *Engine) recordEvent(ctx context.Context, name string, attrs map[string]string) {
	if e.Telemetry != nil {
		e.Telemetry.RecordEvent(ctx, name, attrs)
	}
}

// artifactDir resolves (and creates) the artifact dir for one run.
func (e *Engine) artifactDir(ctx context.Context, runID string) (string, error) {
	workDir, _ := workspace.WorkLayout(e.Workspace)
	rel := workspace.JoinWork(workDir, "workflow/"+runID)
	dir, err := e.Workspace.Resolve(ctx, rel)
	if err != nil {
		return "", fmt.Errorf("resolve artifact dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}
	return dir, nil
}

// artifactPath returns the absolute path for a step output file.
func (e *Engine) artifactPath(ctx context.Context, runID, stepID, ext string) (string, error) {
	dir, err := e.artifactDir(ctx, runID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, stepID+ext), nil
}

// spillView caps an inline view; oversized content is written to an artifact
// file and the returned path references it.
func (e *Engine) spillView(ctx context.Context, runID, stepID, ext, content string) (view, path string) {
	if len(content) <= MaxViewBytes {
		return content, ""
	}
	path, err := e.artifactPath(ctx, runID, stepID, ext)
	if err != nil {
		slog.Warn("workflow artifact write failed; inline view truncated", "step", stepID, "error", err)
		return truncateRunes(content, MaxViewBytes), ""
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		slog.Warn("workflow artifact write failed; inline view truncated", "step", stepID, "path", path, "error", err)
		return truncateRunes(content, MaxViewBytes), ""
	}
	return truncateRunes(content, MaxViewBytes) + "\n...[Full output: " + path + "]", path
}

func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return string([]rune(s)[:max/2]) + "..."
}

// parseJSONContent returns the decoded value when content is a JSON
// object/array, else nil.
func parseJSONContent(content string) any {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return nil
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return nil
	}
	return value
}
