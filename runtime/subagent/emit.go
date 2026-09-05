package subagent

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// forwardParentEmit returns an emit hook for a child agent that forwards only
// tool-call streaming to the parent's outbound hook. Text and thinking deltas
// stay suppressed so the parent's answer stream is not interleaved.
func forwardParentEmit(ctx context.Context, parent agentkit.OutboundEmit) agentkit.OutboundEmit {
	if parent == nil {
		return nil
	}
	parentRoute := session.RouteRefFromContext(ctx)
	id, ok := session.RouteSessionID(parentRoute)
	if !ok || id == "" {
		return nil
	}
	return func(emitCtx context.Context, event agentkit.OutboundEvent) error {
		switch event.Type {
		case agentkit.EventToolResult:
			event.Route = parentRoute
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
		event.Route = parentRoute
		return parent(ctx, event)
	}
}

func emitFromContext(ctx context.Context) agentkit.OutboundEmit {
	return agentkit.OutboundEmitFromContext(ctx)
}

// emitSubagentLifecycle forwards subagent/start and subagent/end to the parent
// delivery session so platforms can render delegation in the progress card.
func emitSubagentLifecycle(ctx context.Context, parentAgent agentkit.AgentID, typ agentkit.EventType, data any) error {
	emit := emitFromContext(ctx)
	if emit == nil {
		return nil
	}
	parentRoute := session.RouteRefFromContext(ctx)
	id, ok := session.RouteSessionID(parentRoute)
	if !ok || id == "" {
		return nil
	}
	agentID := parentAgent
	if agentID == "" {
		agentID = session.AgentIDFromContext(ctx)
	}
	return emit(ctx, agentkit.OutboundEvent{
		Route:   parentRoute,
		AgentID: agentID,
		Type:    typ,
		Data:    agentkit.MarshalOutboundData(data),
	})
}
