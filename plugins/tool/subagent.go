package tool

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
)

type SubagentConfig struct{}

type SubagentDeps struct {
	Subagent subagent.Spawner `json:"subagent"`
}

type SubagentInput struct {
	Agent string `json:"agent" jsonschema:"required,description=Name of the subagent to delegate to, from the subagent list in the system prompt"`
	Task  string `json:"task" jsonschema:"required,description=Self-contained instructions. The subagent starts from an empty session and cannot see this conversation"`
}

type SubagentOutput struct {
	Agent   string `json:"agent"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Session string `json:"session"`
	Steps   int    `json:"steps"`
}

// NewSubagent registers tool/subagent: Delegate a subtask to a child agent (tool name: delegate) and wait for its conclusion.
//
// Best practices:
//   - Pair with prompt/section/subagents, which lists the valid agent names; this tool's description is static and cannot.
//   - Mount it only on the main agent's tools runtime. The subagent spawner needs a separate runtime without it, both to break a dependency cycle and to keep children from delegating further.
//   - Bump toolTimeouts for delegate: a child agent runs many steps and will blow through the default tool timeout.
func NewSubagent(_ SubagentConfig, deps SubagentDeps) (agentkit.ToolPack, error) {
	if deps.Subagent == nil {
		return nil, fmt.Errorf("tool/subagent requires subagent dependency")
	}
	spawner := deps.Subagent
	tool, err := agentkit.NewTool[SubagentInput, SubagentOutput]("delegate", func(ctx context.Context, input SubagentInput) (SubagentOutput, error) {
		result, err := spawner.Run(ctx, subagent.Request{Agent: input.Agent, Task: input.Task})
		if err != nil {
			return SubagentOutput{}, err
		}
		return SubagentOutput{
			Agent:   result.Agent,
			Status:  result.Status,
			Summary: result.Summary,
			Session: result.Session,
			Steps:   result.Steps,
		}, nil
	}).
		Description("Delegate a self-contained subtask to one of the subagents listed in the system prompt, and wait for its conclusion. " +
			"Use this to keep bulky exploration out of this conversation: the subagent's own steps stay in its session and only its summary comes back. " +
			"The subagent cannot see this conversation, so put every fact it needs into task. It runs to completion before you regain control.").
		Build()
	if err != nil {
		return nil, err
	}
	return agentkit.Pack(tool), nil
}
