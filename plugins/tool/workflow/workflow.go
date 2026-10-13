// Package workflow registers tool/workflow: run a named declarative pipeline
// (script / tool / model / agent steps) with model-small name+args input, so
// large payloads flow between steps without crossing the main model.
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	// Telemetry receives workflow.run / workflow.step events.
	Telemetry captelemetry.Toolkit `json:"telemetry,omitempty"`
}

// NewWorkflow registers tool/workflow: Run a fixed pipeline from the workflows directory by name.
//
// Best practices:
//   - Point deps.runtime at a sibling tools/runtime that includes tool/shell-bash (bash) and the tools your steps call, and never includes tool/workflow itself.
//   - Keep step data exchange by reference: scripts and tools read/write files; the model only sees the rendered result.
//   - Mount through tools/runtime deps.tools; the "workflow" slash command is contributed via CommandProvider on the same instance.
func NewWorkflow(cfg WorkflowConfig, deps WorkflowDeps) (agentkit.Tool, error) {
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
	return &workflowTool{dirs: dirs, deps: deps}, nil
}

type workflowTool struct {
	dirs []string
	deps WorkflowDeps
}

type workflowInput struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

func (t *workflowTool) Name() string { return "workflow" }

func (t *workflowTool) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{
		Type: "object",
		Properties: map[string]agentkit.JSONSchema{
			"name": {Type: "string", Description: "Workflow name (file name without .json in the workflows directory)"},
			"args": {Type: "object", Description: "Arguments matching the workflow's declared inputs"},
		},
		Required: []string{"name"},
	}
}

// Description lists the currently discovered workflows; tools/runtime calls it
// when assembling the catalog, so edits to workflow files show up next turn.
func (t *workflowTool) Description() string {
	var b strings.Builder
	b.WriteString("Run a fixed pipeline (script/tool/model/agent steps) by name. " +
		"Intermediate data stays in files and tool calls; only the pipeline's rendered result returns here, " +
		"so use workflows for large payloads that must not pass through the conversation. " +
		"Available workflows:\n")
	workflows, errs := Discover(context.Background(), t.deps.Workspace, t.dirs)
	if len(workflows) == 0 {
		b.WriteString("(none found; check the workflows directory)\n")
	}
	for _, wf := range workflows {
		fmt.Fprintf(&b, "- %s: %s", wf.Name, wf.Description)
		if schema := wf.InputsJSONSchema(); schema != "" {
			fmt.Fprintf(&b, " args schema: %s", schema)
		}
		b.WriteString("\n")
	}
	for _, err := range errs {
		fmt.Fprintf(&b, "- [load error] %v\n", err)
	}
	return b.String()
}

func (t *workflowTool) Call(ctx context.Context, raw json.RawMessage) (string, error) {
	var input workflowInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("invalid workflow input: %v", err), nil
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return "workflow name is required", nil
	}
	wf, err := Find(ctx, t.deps.Workspace, t.dirs, name)
	if err != nil {
		return err.Error(), nil
	}
	args := map[string]any{}
	if len(input.Args) > 0 {
		if err := json.Unmarshal(input.Args, &args); err != nil {
			return fmt.Sprintf("workflow %q: args must be a JSON object: %v", name, err), nil
		}
	}
	engine := &Engine{
		Tools:     t.deps.Runtime,
		Workspace: t.deps.Workspace,
		LLM:       t.deps.LLM,
		Subagent:  t.deps.Subagent,
		Telemetry: t.deps.Telemetry,
	}
	result, err := engine.Run(ctx, wf, args)
	if err != nil {
		return err.Error(), nil
	}
	return result, nil
}

// Commands contributes /workflow for human inspection (list discovered files).
func (t *workflowTool) Commands() []agentkit.Command {
	return []agentkit.Command{&workflowCommand{tool: t}}
}

var _ agentkit.CommandProvider = (*workflowTool)(nil)

type workflowCommand struct {
	tool *workflowTool
}

func (c *workflowCommand) Name() string        { return "workflow" }
func (c *workflowCommand) Alias() string       { return "wf" }
func (c *workflowCommand) Description() string { return "List discovered workflows" }

func (c *workflowCommand) CommandExec(ctx context.Context, _ string) (string, error) {
	workflows, errs := Discover(ctx, c.tool.deps.Workspace, c.tool.dirs)
	var b strings.Builder
	if len(workflows) == 0 {
		b.WriteString("no workflows found\n")
	}
	for _, wf := range workflows {
		fmt.Fprintf(&b, "%s\t%d steps\t%s\n  %s\n", wf.Name, len(wf.Steps), wf.Path, wf.Description)
	}
	for _, err := range errs {
		fmt.Fprintf(&b, "[load error] %v\n", err)
	}
	return b.String(), nil
}
