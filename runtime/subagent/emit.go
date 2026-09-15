package subagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"unicode/utf8"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/session"
)

const (
	maxForwardedThinkingDeltaRunes = 96
	maxForwardedThinkingTotalRunes = 480
)

// forwardParentEmit returns an emit hook for a child agent that forwards progress
// signals to the parent's outbound hook: tool call start/end/delta, condensed
// thinking deltas (including child text_delta remapped to thinking_delta for
// progress cards), and tool results. Raw text_delta is never forwarded as body
// text, so the parent's answer stream is not interleaved.
func forwardParentEmit(ctx context.Context, parent agentkit.OutboundEmit) agentkit.OutboundEmit {
	if parent == nil {
		return nil
	}
	parentRoute := session.RouteRefFromContext(ctx)
	id, ok := session.RouteSessionID(parentRoute)
	if !ok || id == "" {
		return nil
	}
	f := &parentEmitForwarder{
		ctx:         ctx,
		parent:      parent,
		parentRoute: parentRoute,
	}
	return f.emit
}

type parentEmitForwarder struct {
	ctx           context.Context
	parent        agentkit.OutboundEmit
	parentRoute   agentkit.RouteRef
	thinkingRunes int
}

func (f *parentEmitForwarder) emit(_ context.Context, event agentkit.OutboundEvent) error {
	switch event.Type {
	case agentkit.EventToolResult:
		event.Route = f.parentRoute
		return f.parent(f.ctx, event)
	case agentkit.EventMessageUpdate:
		var payload agentkit.MessageUpdatePayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return nil
		}
		ame := payload.AssistantMessageEvent
		if !f.shouldForwardMessageUpdate(ame) {
			return nil
		}
		if ame.Type == agentkit.AssistantEventTextDelta {
			condensed := f.condenseThinkingDelta(ame.Delta)
			if condensed == "" {
				return nil
			}
			payload.AssistantMessageEvent.Type = agentkit.AssistantEventThinkingDelta
			payload.AssistantMessageEvent.ContentIndex = 1
			payload.AssistantMessageEvent.Delta = condensed
			event.Data = loop.MarshalOutboundData(payload)
		} else if ame.Type == agentkit.AssistantEventThinkingDelta {
			condensed := f.condenseThinkingDelta(ame.Delta)
			if condensed == "" {
				return nil
			}
			payload.AssistantMessageEvent.Delta = condensed
			event.Data = loop.MarshalOutboundData(payload)
		}
		event.Route = f.parentRoute
		return f.parent(f.ctx, event)
	default:
		return nil
	}
}

func (f *parentEmitForwarder) shouldForwardMessageUpdate(ame agentkit.AssistantMessageEvent) bool {
	switch ame.Type {
	case agentkit.AssistantEventToolCallStart,
		agentkit.AssistantEventToolCallEnd,
		agentkit.AssistantEventToolCallDelta:
		return true
	case agentkit.AssistantEventThinkingDelta,
		agentkit.AssistantEventTextDelta:
		return ame.Delta != "" && f.thinkingRunes < maxForwardedThinkingTotalRunes
	default:
		return false
	}
}

func (f *parentEmitForwarder) condenseThinkingDelta(delta string) string {
	if delta == "" || f.thinkingRunes >= maxForwardedThinkingTotalRunes {
		return ""
	}
	remaining := maxForwardedThinkingTotalRunes - f.thinkingRunes
	chunkMax := maxForwardedThinkingDeltaRunes
	if chunkMax > remaining {
		chunkMax = remaining
	}
	out := truncateRunes(delta, chunkMax)
	f.thinkingRunes += utf8.RuneCountInString(out)
	return out
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func emitFromContext(ctx context.Context) agentkit.OutboundEmit {
	return loop.OutboundEmitFromContext(ctx)
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
	event := agentkit.OutboundEvent{
		Route:   parentRoute,
		AgentID: agentID,
		Type:    typ,
		Data:    loop.MarshalOutboundData(data),
	}
	go func() {
		emitCtx := context.WithoutCancel(ctx)
		slog.Debug("subagent lifecycle outbound", "event", typ, "route", parentRoute)
		if err := emit(emitCtx, event); err != nil {
			slog.Warn("subagent lifecycle outbound failed (session audit already recorded)", "event", typ, "err", err)
		}
	}()
	return nil
}
