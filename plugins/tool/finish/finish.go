package finish

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

type FinishConfig struct{}

type FinishDeps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

type FinishInput struct {
	Status  string `json:"status,omitempty" jsonschema:"completed when the task is done; blocked when it cannot proceed"`
	Summary string `json:"summary" jsonschema:"What was accomplished, or what is blocking"`
}

type FinishOutput struct {
	Status       string `json:"status"`
	Acknowledged bool   `json:"acknowledged"`
}

// NewFinish registers tool/finish: End an autonomous run, with status=completed or status=blocked.
//
// Best practices:
//   - This is the primary signal that a worker run completed cleanly; hook/turn-continue also stops on stall or continuation limit.
func NewFinish(_ FinishConfig, deps FinishDeps) (agentkit.Tool, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("tool/finish requires sessionStore dependency")
	}
	store := deps.SessionStore
	tool, err := agentkit.NewTool[FinishInput, FinishOutput]("finish", func(ctx context.Context, input FinishInput) (FinishOutput, error) {
		summary := strings.TrimSpace(input.Summary)
		if summary == "" {
			return FinishOutput{}, fmt.Errorf("finish requires a summary")
		}
		sessionID := rctx.SessionIDFromContext(ctx)
		if sessionID == "" {
			return FinishOutput{}, fmt.Errorf("finish requires a session")
		}
		agentID := rctx.AgentIDFromContext(ctx)
		sess, err := store.Get(ctx, sessionID)
		if err != nil {
			return FinishOutput{}, err
		}
		status := sessevents.FinishCompleted
		if strings.EqualFold(strings.TrimSpace(input.Status), sessevents.FinishBlocked) {
			status = sessevents.FinishBlocked
		}
		if err := sessevents.AppendRunFinish(ctx, sess, agentID, sessevents.RunFinishData{
			Status:  status,
			Summary: summary,
		}); err != nil {
			return FinishOutput{}, err
		}
		return FinishOutput{Status: status, Acknowledged: true}, nil
	}).
		Description("End the run. Call this once the task is complete (status=completed) or cannot continue (status=blocked), with a summary of the outcome.").
		Build()
	if err != nil {
		return nil, err
	}
	return tool, nil
}
