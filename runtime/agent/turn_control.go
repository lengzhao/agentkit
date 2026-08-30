package agent

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// turnControl is the step-level view Loop attaches via KeySessionControl.
type turnControl interface {
	ClearTurnCancel()
	BeginStep(parent context.Context) (context.Context, func())
	PopCancelReason() string
	PopSteering() []agentkit.ModelMessage
	HasSteering() bool
}

func turnControlFrom(ctx context.Context) turnControl {
	if v, ok := ctx.Value(agentkit.KeySessionControl).(turnControl); ok && v != nil {
		return v
	}
	return noopTurnControl{}
}

type noopTurnControl struct{}

func (noopTurnControl) ClearTurnCancel() {}

func (noopTurnControl) BeginStep(parent context.Context) (context.Context, func()) {
	return parent, func() {}
}

func (noopTurnControl) PopCancelReason() string { return "" }

func (noopTurnControl) PopSteering() []agentkit.ModelMessage { return nil }

func (noopTurnControl) HasSteering() bool { return false }
