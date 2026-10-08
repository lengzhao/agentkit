package subagent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// maxDelegateDepth is the maximum number of delegate calls in one chain
// (3 layers: main agent + 2 subagents). Depth is inferred from nested
// subagent session ids (sub:sub:...).
const maxDelegateDepth = 2

func delegationDepth(ctx context.Context) int {
	sessionID := rctx.SessionIDFromContext(ctx)
	return strings.Count(string(sessionID), ":sub:")
}

type SubagentConfig struct {
	// DefaultTimeoutSeconds applies when the model omits timeoutSeconds on delegate.
	DefaultTimeoutSeconds int `json:"defaultTimeoutSeconds,omitempty"`
}

// SetDefaults implements pluginkit.Defaulter.
func (c *SubagentConfig) SetDefaults() {
	if c.DefaultTimeoutSeconds <= 0 {
		c.DefaultTimeoutSeconds = subagent.DefaultDelegationTimeoutSeconds
	}
}

type SubagentDeps struct {
	Subagent subagent.Spawner `json:"subagent"`
}

type SubagentInput struct {
	Agent string `json:"agent" jsonschema:"Name of the subagent to delegate to, from the subagent list in the system prompt"`
	Task  string `json:"task" jsonschema:"Self-contained instructions. The subagent starts from an empty session and cannot see this conversation"`
	Async *bool  `json:"async,omitempty" jsonschema:"When true, return immediately and deliver the conclusion in a follow-up turn. Omit to use the subagent default."`
	// TimeoutSeconds overrides the spawner wall clock for this call and extends the delegate tool wait when larger than toolTimeouts.
	TimeoutSeconds *int `json:"timeoutSeconds,omitempty" jsonschema:"Wall-clock limit in seconds for this delegation. Default 900 when omitted."`
}

type SubagentOutput struct {
	Agent   string `json:"agent"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Session string `json:"session"`
	Steps   int    `json:"steps"`
	JobID   string `json:"jobId,omitempty"`
}

// NewSubagent registers tool/subagent: Delegate a subtask to a child agent (tool name: delegate) and wait for its conclusion.
//
// Best practices:
//   - Pair with prompt/section/subagents, which lists the valid agent names; this tool's description is static and cannot.
//   - Mount it only on the main agent's tools runtime. The subagent spawner needs a separate runtime without it, both to break a dependency cycle and to keep children from delegating further.
//   - Bump toolTimeouts for delegate: a child agent runs many steps and will blow through the default tool timeout.
func NewSubagent(cfg SubagentConfig, deps SubagentDeps) (agentkit.Tool, error) {
	cfg.SetDefaults()
	if deps.Subagent == nil {
		return nil, fmt.Errorf("tool/subagent requires subagent dependency")
	}
	spawner := deps.Subagent
	defaultTimeout := cfg.DefaultTimeoutSeconds
	tool, err := agentkit.NewTool[SubagentInput, SubagentOutput]("delegate", func(ctx context.Context, input SubagentInput) (SubagentOutput, error) {
		if depth := delegationDepth(ctx); depth >= maxDelegateDepth {
			return SubagentOutput{}, fmt.Errorf("delegation depth limit reached (%d); at most %d delegate calls allowed", depth, maxDelegateDepth)
		}
		parent := rctx.SessionIDFromContext(ctx)
		slog.Info("delegate: start", "agent", input.Agent, "parent", parent, "async", input.Async)
		timeoutSeconds := input.TimeoutSeconds
		if timeoutSeconds == nil {
			timeoutSeconds = &defaultTimeout
		}
		started := time.Now()
		result, err := spawner.Run(ctx, subagent.Request{
			Agent:          input.Agent,
			Task:           input.Task,
			Async:          input.Async,
			TimeoutSeconds: timeoutSeconds,
		})
		elapsed := time.Since(started)
		if err != nil {
			slog.Warn("delegate: failed", "agent", input.Agent, "parent", parent, "duration", elapsed, "err", err)
			return SubagentOutput{}, err
		}
		slog.Info("delegate: done", "agent", input.Agent, "parent", parent, "status", result.Status, "child", result.Session, "duration", elapsed)
		return SubagentOutput{
			Agent:   result.Agent,
			Status:  result.Status,
			Summary: result.Summary,
			Session: result.Session,
			Steps:   result.Steps,
			JobID:   result.JobID,
		}, nil
	}).
		Description("Delegate a self-contained subtask to one of the subagents listed in the system prompt. " +
			"Use timeoutSeconds for a longer or shorter wall clock than the subagent default (still subject to toolTimeouts.delegate unless you raise it). " +
			"Use async=true for long-running loop agents (e.g. cursor) so you regain control immediately and receive the conclusion in a follow-up turn. " +
			"Use this to keep bulky exploration out of this conversation: the subagent's own steps stay in its session and only its summary comes back. " +
			"The subagent cannot see this conversation, so put every fact it needs into task.").
		Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}
