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
		switch event.Type {
		case agentkit.EventToolResult:
			event.SessionID = parentSession
			return parent(ctx, event)
		case agentkit.EventMessageUpdate:
		default:
			return nil
		}
		var payload agentkit.MessageUpdatePayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return nil
		}
		if payload.AssistantMessageEvent.Type != agentkit.AssistantEventToolCallEnd {
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

func emitFromContext(ctx context.Context) agentkit.OutboundEmit {
	emit, _ := ctx.Value(agentkit.KeyOutboundEmit).(agentkit.OutboundEmit)
	return emit
}

// emitSubagentLifecycle forwards subagent/start and subagent/end to the parent
// delivery session so platforms can render delegation in the progress card.
func emitSubagentLifecycle(ctx context.Context, parentAgent agentkit.AgentID, typ agentkit.EventType, data any) error {
	emit := emitFromContext(ctx)
	if emit == nil {
		return nil
	}
	sessionID := parentSessionID(ctx)
	if sessionID == "" {
		return nil
	}
	agentID := parentAgent
	if agentID == "" {
		agentID, _ = ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
	}
	return emit(ctx, agentkit.OutboundEvent{
		SessionID: sessionID,
		AgentID:   agentID,
		Type:      typ,
		Data:      agentkit.MarshalOutboundData(data),
	})
}
