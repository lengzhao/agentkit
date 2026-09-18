package loop

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

// wrapTurnEndNotices emits user-visible outbound (e.g. step-limit) before turn/end,
// similar to how platforms render cancel from turn/end metadata.
func wrapTurnEndNotices(emit agentkit.OutboundEmit) agentkit.OutboundEmit {
	if emit == nil {
		return nil
	}
	return func(ctx context.Context, event agentkit.OutboundEvent) error {
		if event.Type == agentkit.EventTurnEnd {
			var end sessevents.TurnEndData
			if err := json.Unmarshal(event.Data, &end); err == nil {
				if err := emitStepLimitNotice(ctx, emit, event, end); err != nil {
					return err
				}
			}
		}
		return emit(ctx, event)
	}
}

func emitStepLimitNotice(ctx context.Context, emit agentkit.OutboundEmit, turnEnd agentkit.OutboundEvent, end sessevents.TurnEndData) error {
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
