// Package workflow loads and runs declarative pipelines (workflow files) that
// chain scripts, tool calls, one-shot model completions, and subagent
// delegations without routing intermediate data through the main model.
//
// Design: docs/guides/workflow.zh.md. The invariant is that the main
// conversation never touches large payloads: steps exchange data by reference
// (artifact files under work/workflow/<run>/) and only the rendered result
// template crosses back to the model.
package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Step types.
const (
	StepScript = "script"
	StepTool   = "tool"
	StepModel  = "model"
	StepAgent  = "agent"
)

// Workflow is one parsed pipeline file. Name comes from the file name.
type Workflow struct {
	Name        string         `json:"-"`
	Description string         `json:"description"`
	Inputs      map[string]any `json:"inputs"`
	Steps       []Step         `json:"steps"`
	Result      string         `json:"result"`
	// Dir is the directory holding the workflow file; relative script paths
	// resolve against it.
	Dir string `json:"-"`
	// Path is the workflow file itself, for error messages.
	Path string `json:"-"`
}

// Step is one pipeline stage. Fields are type-specific; see the design doc.
type Step struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// ContinueOnError records the failure in step state and runs the next step.
	ContinueOnError bool `json:"continueOnError,omitempty"`
	TimeoutSeconds  int  `json:"timeoutSeconds,omitempty"`

	// script
	Run string `json:"run,omitempty"`
	Dir string `json:"dir,omitempty"`

	// tool
	Tool string         `json:"tool,omitempty"`
	Args map[string]any `json:"args,omitempty"`

	// model
	Model         string         `json:"model,omitempty"`
	Prompt        string         `json:"prompt,omitempty"`
	OutputSchema  map[string]any `json:"outputSchema,omitempty"`
	MaxInputBytes int            `json:"maxInputBytes,omitempty"`

	// agent
	Agent string `json:"agent,omitempty"`
	Task  string `json:"task,omitempty"`
}

// Parse decodes and validates one workflow file body.
func Parse(name string, data []byte) (*Workflow, error) {
	var wf Workflow
	if err := json.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("workflow %q: invalid JSON: %w", name, err)
	}
	wf.Name = name
	if err := wf.validate(); err != nil {
		return nil, err
	}
	return &wf, nil
}

func (wf *Workflow) validate() error {
	if strings.TrimSpace(wf.Description) == "" {
		return fmt.Errorf("workflow %q: description is required (the main model picks workflows by it)", wf.Name)
	}
	if len(wf.Steps) == 0 {
		return fmt.Errorf("workflow %q: at least one step is required", wf.Name)
	}
	seen := make(map[string]bool, len(wf.Steps))
	for i, step := range wf.Steps {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("workflow %q: step %d has no id", wf.Name, i)
		}
		if seen[step.ID] {
			return fmt.Errorf("workflow %q: duplicate step id %q", wf.Name, step.ID)
		}
		seen[step.ID] = true
		if err := step.validate(wf.Name); err != nil {
			return err
		}
	}
	if strings.TrimSpace(wf.Result) == "" {
		return fmt.Errorf("workflow %q: result template is required", wf.Name)
	}
	return nil
}

func (s Step) validate(wfName string) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("workflow %q step %q: %s", wfName, s.ID, fmt.Sprintf(format, args...))
	}
	if s.TimeoutSeconds < 0 {
		return fail("timeoutSeconds must not be negative")
	}
	switch s.Type {
	case StepScript:
		if strings.TrimSpace(s.Run) == "" {
			return fail("script step requires run")
		}
	case StepTool:
		if strings.TrimSpace(s.Tool) == "" {
			return fail("tool step requires tool")
		}
	case StepModel:
		if strings.TrimSpace(s.Prompt) == "" {
			return fail("model step requires prompt")
		}
		if s.MaxInputBytes < 0 {
			return fail("maxInputBytes must not be negative")
		}
	case StepAgent:
		if strings.TrimSpace(s.Agent) == "" {
			return fail("agent step requires agent")
		}
		if strings.TrimSpace(s.Task) == "" {
			return fail("agent step requires task")
		}
	default:
		return fail("unknown step type %q (want script, tool, model, or agent)", s.Type)
	}
	return nil
}

// InputsJSONSchema returns the declared inputs schema as a JSON string for the
// tool description; empty when no inputs are declared.
func (wf *Workflow) InputsJSONSchema() string {
	if len(wf.Inputs) == 0 {
		return ""
	}
	raw, err := json.Marshal(wf.Inputs)
	if err != nil {
		return ""
	}
	return string(raw)
}
