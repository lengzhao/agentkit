package sessevents

import (
	"context"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

func (events) AppendTodoUpdate(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, items []capsession.Todo) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventTodoUpdate, capsession.TodoUpdateData{Items: items})
}

func (events) AppendRunFinish(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data capsession.RunFinishData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventRunFinish, data)
}

func (events) AppendTurnContinue(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data capsession.TurnContinueData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventTurnContinue, data)
}

func (events) AppendUsage(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data capsession.UsageData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventUsage, data)
}
