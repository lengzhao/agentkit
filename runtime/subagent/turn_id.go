package subagent

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
)

func parentTurnID(ctx context.Context) string {
	if v, ok := ctx.Value(agentkit.KeyTurnID).(string); ok {
		if id := strings.TrimSpace(v); id != "" {
			return id
		}
	}
	return strings.TrimSpace(rttelemetry.TurnIDFrom(ctx))
}

func withParentTurnID(ctx context.Context, event agentkit.OutboundEvent) agentkit.OutboundEvent {
	if strings.TrimSpace(event.TurnID) != "" {
		return event
	}
	if id := parentTurnID(ctx); id != "" {
		event.TurnID = id
	}
	return event
}
