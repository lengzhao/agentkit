package acpplatform

import (
	"context"
	"encoding/json"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
)

type turnEmitter struct {
	platform     *Platform
	acpSessionID acp.SessionId
	conn         *acp.AgentSideConnection
}

func (e *turnEmitter) handleUpdate(ctx context.Context, payload agentkit.MessageUpdatePayload) error {
	ame := payload.AssistantMessageEvent
	switch ame.Type {
	case agentkit.AssistantEventTextDelta:
		if ame.Delta == "" {
			return nil
		}
		return e.sessionUpdate(ctx, acp.UpdateAgentMessageText(ame.Delta))
	case agentkit.AssistantEventThinkingDelta:
		if ame.Delta == "" {
			return nil
		}
		return e.sessionUpdate(ctx, acp.SessionUpdate{
			AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
				Content: acp.TextBlock(ame.Delta),
			},
		})
	case agentkit.AssistantEventToolCallStart:
		id := ame.ID
		title := ame.ToolName
		var rawInput any
		if ame.ToolCall != nil {
			if id == "" {
				id = string(ame.ToolCall.ID)
			}
			if title == "" {
				title = ame.ToolCall.Name
			}
			if len(ame.ToolCall.Input) > 0 {
				var m map[string]any
				if err := json.Unmarshal(ame.ToolCall.Input, &m); err == nil {
					rawInput = m
				}
			}
		}
		if id == "" {
			return nil
		}
		opts := []acp.ToolCallStartOpt{
			acp.WithStartStatus(acp.ToolCallStatusPending),
		}
		if rawInput != nil {
			opts = append(opts, acp.WithStartRawInput(rawInput))
		}
		return e.sessionUpdate(ctx, acp.StartToolCall(acp.ToolCallId(id), title, opts...))
	case agentkit.AssistantEventToolCallEnd:
		id := ame.ID
		if ame.ToolCall != nil && id == "" {
			id = string(ame.ToolCall.ID)
		}
		if id == "" {
			return nil
		}
		return e.sessionUpdate(ctx, acp.UpdateToolCall(
			acp.ToolCallId(id),
			acp.WithUpdateStatus(acp.ToolCallStatusCompleted),
		))
	default:
		return nil
	}
}

func (e *turnEmitter) sessionUpdate(ctx context.Context, update acp.SessionUpdate) error {
	return e.conn.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: e.acpSessionID,
		Update:    update,
	})
}
