package session

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
)

type TurnStartData struct{}

type TurnEndData struct {
	Steps int `json:"steps"`
}

type StepStartData struct {
	Step int `json:"step"`
}

type StepEndData struct {
	Step int `json:"step"`
}

func AppendTurnStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventTurnStart, TurnStartData{})
}

func AppendTurnEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, steps int) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventTurnEnd, TurnEndData{Steps: steps})
}

func AppendStepStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, step int) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventStepStart, StepStartData{Step: step})
}

func AppendStepEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, step int) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventStepEnd, StepEndData{Step: step})
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
