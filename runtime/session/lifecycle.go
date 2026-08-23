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

type AutoRetryStartData struct {
	Attempt      int    `json:"attempt"`
	MaxAttempts  int    `json:"maxAttempts"`
	DelayMs      int    `json:"delayMs"`
	ErrorMessage string `json:"errorMessage"`
}

type AutoRetryEndData struct {
	Success    bool   `json:"success"`
	Attempt    int    `json:"attempt"`
	FinalError string `json:"finalError,omitempty"`
}

type SummarizationRetryStartData struct {
	Attempt      int    `json:"attempt"`
	MaxAttempts  int    `json:"maxAttempts"`
	DelayMs      int    `json:"delayMs"`
	ErrorMessage string `json:"errorMessage"`
}

type SummarizationRetryEndData struct {
	Success    bool   `json:"success"`
	Attempt    int    `json:"attempt"`
	FinalError string `json:"finalError,omitempty"`
}

type OverflowRecoveryData struct {
	Applied int    `json:"applied"`
	Reason  string `json:"reason,omitempty"`
	Error   string `json:"error,omitempty"`
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

func AppendAutoRetryStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data AutoRetryStartData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventAutoRetryStart, data)
}

func AppendAutoRetryEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data AutoRetryEndData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventAutoRetryEnd, data)
}

func AppendSummarizationRetryStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data SummarizationRetryStartData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventSummarizationRetryStart, data)
}

func AppendSummarizationRetryEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data SummarizationRetryEndData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventSummarizationRetryEnd, data)
}

func AppendOverflowRecovery(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data OverflowRecoveryData) error {
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
