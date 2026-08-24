package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

type FinishConfig struct{}

type FinishDeps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

type FinishInput struct {
	Status  string `json:"status" jsonschema:"description=completed when the task is done; blocked when it cannot proceed"`
	Summary string `json:"summary" jsonschema:"required,description=What was accomplished, or what is blocking"`
}

type FinishOutput struct {
	Status       string `json:"status"`
	Acknowledged bool   `json:"acknowledged"`
}

// NewFinish builds the explicit run terminator. Recording a run/finish event is
// the only signal that ends an autonomous run early; without it a run stops only
// on budget exhaustion or stall detection.
func NewFinish(_ FinishConfig, deps FinishDeps) (agentkit.Tool, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("tool/finish requires sessionStore dependency")
	}
	store := deps.SessionStore
	return agentkit.NewTool[FinishInput, FinishOutput]("finish", func(ctx context.Context, input FinishInput) (FinishOutput, error) {
		summary := strings.TrimSpace(input.Summary)
		if summary == "" {
			return FinishOutput{}, fmt.Errorf("finish requires a summary")
		}
		sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
		if sessionID == "" {
			return FinishOutput{}, fmt.Errorf("finish requires a session")
		}
		agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
		sess, err := store.Get(ctx, sessionID)
		if err != nil {
			return FinishOutput{}, err
		}
		status := session.FinishCompleted
		if strings.EqualFold(strings.TrimSpace(input.Status), session.FinishBlocked) {
			status = session.FinishBlocked
		}
		if err := session.AppendRunFinish(ctx, sess, agentID, session.RunFinishData{
			Status:  status,
			Summary: summary,
		}); err != nil {
			return FinishOutput{}, err
		}
		return FinishOutput{Status: status, Acknowledged: true}, nil
	}).
		Description("End the run. Call this once the task is complete (status=completed) or cannot continue (status=blocked), with a summary of the outcome.").
		Build()
}
