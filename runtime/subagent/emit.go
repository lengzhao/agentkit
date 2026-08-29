package subagent

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
)

// forwardParentEmit returns an emit hook for a child agent that forwards only
// tool-call streaming to the parent's outbound hook. Text and thinking deltas
// stay suppressed so the parent's answer stream is not interleaved.
func forwardParentEmit(ctx context.Context, parent agentkit.OutboundEmit) agentkit.OutboundEmit {
	if parent == nil {
		return nil
	}
	parentSession := parentSessionID(ctx)
	if parentSession == "" {
		return nil
	}
	return func(emitCtx context.Context, event agentkit.OutboundEvent) error {
		if event.Type != agentkit.EventMessageUpdate {
			return nil
		}
		var payload agentkit.MessageUpdatePayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return nil
		}
		switch payload.AssistantMessageEvent.Type {
		case agentkit.AssistantEventToolCallStart, agentkit.AssistantEventToolCallEnd:
		default:
			return nil
		}
		event.SessionID = parentSession
		return parent(ctx, event)
	}
}

func parentSessionID(ctx context.Context) agentkit.SessionID {
	if delivery, ok := ctx.Value(agentkit.KeyDeliverySessionID).(agentkit.SessionID); ok && delivery != "" {
		return delivery
	}
	if effective, ok := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID); ok {
		return effective
	}
	return ""
}
