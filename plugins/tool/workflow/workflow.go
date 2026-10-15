// Package workflow registers tool/workflow: expose each discovered declarative
// pipeline (script / tool / model / agent steps) as its own dynamic tool, so
// the model calls a workflow directly with a structured input schema while
// large payloads flow between steps without crossing the main model.
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type WorkflowConfig struct {
	// Dirs are scoped workflow directories in precedence order (first dir wins
	// on duplicate names). Defaults to local:workflows then global:workflows.
	Dirs []string `json:"dirs,omitempty"`
}

type WorkflowDeps struct {
	// Runtime executes tool steps; use a sibling tools/runtime instance without
	// tool/workflow itself (cycle break, same pattern as tool/subagent).
	Runtime   agentkit.ToolRuntime `json:"runtime"`
	Workspace workspace.Service    `json:"workspace"`
	// LLM is required only when a workflow uses model steps.
	LLM agentkit.LLMProvider `json:"llm,omitempty"`
	// Subagent is required only when a workflow uses agent steps.
	Subagent subagent.Spawner `json:"subagent,omitempty"`
	// Telemetry receives workflow.run / workflow.step / workflow.script spans.
	Telemetry captelemetry.Toolkit `json:"telemetry,omitempty"`
	// Shell runs script steps with raw output (no model formatting). Inject the
	// same tool/shell-bash instance (StructuredShell); omit to fall back to
	// calling the bash tool through Runtime and parsing its text output.
	Shell StructuredShell `json:"shell,omitempty"`
}

// toolNamePrefix namespaces workflow tools like MCP's server__tool convention.
const toolNamePrefix = "workflow__"

// NewWorkflow registers tool/workflow: one dynamic tool per workflow file.
//
// Best practices:
//   - Mount through tools/runtime deps.dynamicTools; with tools/deferred the workflow tools join MCP/OpenAPI behind the disclosure bridges.
//   - Point deps.runtime at a sibling tools/runtime that includes the tools your steps call, and never includes tool/workflow itself.
//   - For script steps, inject deps.shell with the same tool/shell-bash instance (StructuredShell) to avoid parsing bash text; otherwise the engine calls the bash tool through Runtime.
//   - Keep step data exchange by reference: scripts and tools read/write files; the model only sees the rendered result.
//   - The "workflow" slash command is contributed via CommandProvider on the same instance.
func NewWorkflow(cfg WorkflowConfig, deps WorkflowDeps) (agentkit.ToolProvider, error) {
	if deps.Runtime == nil {
		return nil, fmt.Errorf("tool/workflow requires runtime dependency")
	}
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/workflow requires workspace dependency")
	}
	dirs := cfg.Dirs
	if len(dirs) == 0 {
		dirs = DefaultDirs
	}
	return &workflowProvider{
		dirs: dirs,
		deps: deps,
		engine: &Engine{
			Tools:     deps.Runtime,
			Workspace: deps.Workspace,
			LLM:       deps.LLM,
			Subagent:  deps.Subagent,
			Telemetry: deps.Telemetry,
			Shell:     deps.Shell,
		},
	}, nil
}

type workflowProvider struct {
	dirs   []string
	deps   WorkflowDeps
	engine *Engine
}

// ListTools re-discovers the workflow directories on every call (tools/runtime
// refreshes dynamic providers per turn), so file edits take effect next turn.
func (p *workflowProvider) ListTools(ctx context.Context) ([]agentkit.Tool, error) {
	workflows, errs := Discover(ctx, p.deps.Workspace, p.dirs)
	for _, err := range errs {
		slog.Warn("tool/workflow discovery skipped a file", "err", err)
	}
	tools := make([]agentkit.Tool, 0, len(workflows))
	for _, wf := range workflows {
		tools = append(tools, &workflowRunTool{wf: wf, engine: p.engine})
	}
	return tools, nil
}

// workflowRunTool runs one specific workflow file; args are the workflow inputs.
type workflowRunTool struct {
	wf     *Workflow
	engine *Engine
}

// timeoutSecondsArg is the reserved call argument overriding the workflow
// tool's default execution timeout (see CallTimeout). It is consumed by the
// tool wrapper and never reaches the workflow's declared inputs.
const timeoutSecondsArg = "timeoutSeconds"

func (t *workflowRunTool) Name() string { return toolNamePrefix + sanitizeToolName(t.wf.Name) }

func (t *workflowRunTool) Description() string {
	// The file path lets the model edit this workflow in place; new workflows
	// follow the same directory (see /workflow and the workflow-manager skill).
	// The default timeout lets the model size the outer call against the inner
	// step timeouts (sum of step timeouts + buffer; pass timeoutSeconds to
	// override).
	return fmt.Sprintf("%s\nWorkflow file: %s\nDefault timeout: %ds (pass timeoutSeconds to override)",
		strings.TrimSpace(t.wf.Description), t.wf.Path, t.wf.DefaultTimeoutSeconds())
}

func (t *workflowRunTool) InputSchema() agentkit.JSONSchema {
	schema := make(map[string]any, len(t.wf.Inputs)+2)
	for k, v := range t.wf.Inputs {
		schema[k] = v
	}
	if _, ok := schema["type"]; !ok {
		schema["type"] = "object"
	}
	// Copy the properties map so the reserved argument never leaks into the
	// workflow's declared inputs (engine-side validation uses wf.Inputs).
	srcProps, _ := t.wf.Inputs["properties"].(map[string]any)
	props := make(map[string]any, len(srcProps)+1)
	for k, v := range srcProps {
		props[k] = v
	}
	if _, declared := props[timeoutSecondsArg]; !declared {
		props[timeoutSecondsArg] = map[string]any{
			"type": "integer",
			"description": fmt.Sprintf("Per-call timeout in seconds; defaults to %d (sum of step timeouts + %ds buffer).",
				t.wf.DefaultTimeoutSeconds(), TimeoutBufferSeconds),
		}
	}
	schema["properties"] = props
	return agentkit.JSONSchema{Raw: schema}
}

// CallTimeout implements agentkit.CallTimeoutProvider: the per-call timeout is
// the reserved timeoutSeconds argument, or the summed step timeouts plus a
// buffer when omitted. tools/runtime prefers it over a smaller configured
// timeout, so inner step timeouts always fire before the outer call deadline.
func (t *workflowRunTool) CallTimeout(raw json.RawMessage) time.Duration {
	if secs := timeoutSecondsFrom(raw); secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return time.Duration(t.wf.DefaultTimeoutSeconds()) * time.Second
}

func timeoutSecondsFrom(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var input struct {
		TimeoutSeconds int `json:"timeoutSeconds"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return 0
	}
	return input.TimeoutSeconds
}

func (t *workflowRunTool) Call(ctx context.Context, raw json.RawMessage) (string, error) {
	args := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return fmt.Sprintf("workflow %q: args must be a JSON object: %v", t.wf.Name, err), nil
		}
	}
	delete(args, timeoutSecondsArg) // consumed by CallTimeout, not a workflow input
	result, err := t.engine.Run(ctx, t.wf, args)
	if err != nil {
		return err.Error(), nil
	}
	return result, nil
}

// sanitizeToolName maps a workflow file name to a model-tool-legal name
// (OpenAI-style [a-zA-Z0-9_-]); collisions are dropped by tools/runtime.
func sanitizeToolName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, name)
}

// Commands contributes /workflow for human inspection (list discovered files).
func (p *workflowProvider) Commands() []agentkit.Command {
	return []agentkit.Command{&workflowCommand{provider: p}}
}

var (
	_ agentkit.ToolProvider        = (*workflowProvider)(nil)
	_ agentkit.CommandProvider     = (*workflowProvider)(nil)
	_ agentkit.Tool                = (*workflowRunTool)(nil)
	_ agentkit.CallTimeoutProvider = (*workflowRunTool)(nil)
)

type workflowCommand struct {
	provider *workflowProvider
}

func (c *workflowCommand) Name() string        { return "workflow" }
func (c *workflowCommand) Alias() string       { return "wf" }
func (c *workflowCommand) Description() string { return "List discovered workflows" }

func (c *workflowCommand) CommandExec(ctx context.Context, _ string) (string, error) {
	workflows, errs := Discover(ctx, c.provider.deps.Workspace, c.provider.dirs)
	var b strings.Builder
	if len(workflows) == 0 {
		b.WriteString("no workflows found\n")
	}
	for _, wf := range workflows {
		fmt.Fprintf(&b, "%s\t%d steps\t%s\n  %s\n", toolNamePrefix+sanitizeToolName(wf.Name), len(wf.Steps), wf.Path, wf.Description)
	}
	for _, err := range errs {
		fmt.Fprintf(&b, "[load error] %v\n", err)
	}
	return b.String(), nil
}
