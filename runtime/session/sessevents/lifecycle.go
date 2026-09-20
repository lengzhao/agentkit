package sessevents

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

func (events) AppendTurnStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventTurnStart, capsession.TurnStartData{})
}

func (events) AppendTurnEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data capsession.TurnEndData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventTurnEnd, data)
}

func (events) AppendStepStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, step int) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventStepStart, capsession.StepStartData{Step: step})
}

func (events) AppendStepEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, step int) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventStepEnd, capsession.StepEndData{Step: step})
}

func (events) AppendAutoRetryStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data capsession.RetryStartData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventAutoRetryStart, data)
}

func (events) AppendAutoRetryEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data capsession.RetryEndData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventAutoRetryEnd, data)
}

func (events) AppendSummarizationRetryStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data capsession.RetryStartData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventSummarizationRetryStart, data)
}

func (events) AppendSummarizationRetryEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data capsession.RetryEndData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventSummarizationRetryEnd, data)
}

func (events) AppendOverflowRecovery(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data capsession.OverflowRecoveryData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventOverflowRecovery, data)
}

func appendLifecycle(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, typ agentkit.EventType, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.Append(ctx, agentkit.SessionEvent{
		AgentID: agentID,
		Type:    typ,
		Data:    raw,
	})
	return err
}
