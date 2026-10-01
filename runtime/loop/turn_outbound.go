package loop

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// wrapTurnEndNotices emits user-visible outbound (e.g. step-limit) before turn/end,
// similar to how platforms render cancel from turn/end metadata.
func wrapTurnEndNotices(emit agentkit.OutboundEmit) agentkit.OutboundEmit {
	if emit == nil {
		return nil
	}
	return func(ctx context.Context, event agentkit.OutboundEvent) error {
		if event.Type == agentkit.EventTurnEnd {
			var end capsession.TurnEndData
			if err := json.Unmarshal(event.Data, &end); err == nil {
				if err := emitStepLimitNotice(ctx, emit, event, end); err != nil {
					return err
				}
			}
		}
		return emit(ctx, event)
	}
}

// turnIDFromMetadata extracts a caller-provided turn id from inbound metadata.
func turnIDFromMetadata(metadata map[string]any) string {
	v, ok := metadata[agentkit.MetadataTurnID].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

// stampTurnID tags every outbound event with the current turn id so platforms
// can correlate per-turn resources (SSE sinks, callbacks) without relying on
// conversation/delivery string equality.
func stampTurnID(emit agentkit.OutboundEmit, turnID string) agentkit.OutboundEmit {
	if emit == nil {
		return nil
	}
	return func(ctx context.Context, event agentkit.OutboundEvent) error {
		event.TurnID = turnID
		return emit(ctx, event)
	}
}

func emitStepLimitNotice(ctx context.Context, emit agentkit.OutboundEmit, turnEnd agentkit.OutboundEvent, end capsession.TurnEndData) error {
	if end.StopReason != string(agentkit.StopStepLimit) {
		return nil
	}
	cap := end.StepLimit
	if cap <= 0 {
		cap = end.Steps
	}
	text := agent.StepLimitUserMessage(cap)
	msg := agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}
	return emit(ctx, agentkit.OutboundEvent{
		Route:      turnEnd.Route,
		AgentID:    turnEnd.AgentID,
		PlatformID: turnEnd.PlatformID,
		UserID:     turnEnd.UserID,
		Type:       agentkit.EventAssistantMessage,
		Data:       rctx.MarshalOutboundData(msg),
	})
}
