package acpremote

import (
	"context"
	"strings"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
)

type updateEmitter struct {
	ctx       context.Context
	sessionID agentkit.SessionID
	agentID   agentkit.AgentID
	emit      agentkit.OutboundEmit

	started  bool
	textBuf  strings.Builder
	thought  strings.Builder
}

func newUpdateEmitter(ctx context.Context, sessionID agentkit.SessionID, agentID agentkit.AgentID, emit agentkit.OutboundEmit) *updateEmitter {
	return &updateEmitter{
		ctx:       ctx,
		sessionID: sessionID,
		agentID:   agentID,
		emit:      emit,
	}
}

func (e *updateEmitter) consume(n acp.SessionNotification) error {
	u := n.Update
	switch {
	case u.AgentMessageChunk != nil:
		text := contentText(u.AgentMessageChunk.Content)
		if text == "" {
			return nil
		}
		if err := e.ensureStarted(); err != nil {
			return err
		}
		e.textBuf.WriteString(text)
		return e.emitDelta(agentkit.AssistantEventTextDelta, 0, text)
	case u.AgentThoughtChunk != nil:
		text := contentText(u.AgentThoughtChunk.Content)
		if text == "" {
			return nil
		}
		if err := e.ensureStarted(); err != nil {
			return err
		}
		e.thought.WriteString(text)
		return e.emitDelta(agentkit.AssistantEventThinkingDelta, 1, text)
	case u.ToolCall != nil:
		if err := e.ensureStarted(); err != nil {
			return err
		}
		ame := agentkit.AssistantMessageEvent{
			Type:         agentkit.AssistantEventToolCallStart,
			ContentIndex: 2,
			ID:           string(u.ToolCall.ToolCallId),
		}
		ame.ToolName = u.ToolCall.Title
		return e.emitUpdate(ame)
	case u.ToolCallUpdate != nil:
		if err := e.ensureStarted(); err != nil {
			return err
		}
		ame := agentkit.AssistantMessageEvent{
			Type:         agentkit.AssistantEventToolCallEnd,
			ContentIndex: 2,
			ID:           string(u.ToolCallUpdate.ToolCallId),
		}
		return e.emitUpdate(ame)
	}
	return nil
}

func (e *updateEmitter) finalize() error {
	if !e.started {
		msg := agentkit.ModelMessage{Role: "assistant"}
		if err := e.sendOutbound(agentkit.EventMessageStart, agentkit.MessageStartPayload{Message: msg}); err != nil {
			return err
		}
		e.started = true
	}
	msg := e.assistantMessage()
	return e.sendOutbound(agentkit.EventMessageEnd, agentkit.MessageEndPayload{Message: msg})
}

func (e *updateEmitter) assistantMessage() agentkit.ModelMessage {
	msg := agentkit.ModelMessage{Role: "assistant"}
	if e.textBuf.Len() > 0 {
		msg.Content = []agentkit.ContentPart{{Type: "text", Text: e.textBuf.String()}}
	}
	return msg
}

func (e *updateEmitter) ensureStarted() error {
	if e.started {
		return nil
	}
	e.started = true
	return e.sendOutbound(agentkit.EventMessageStart, agentkit.MessageStartPayload{
		Message: agentkit.ModelMessage{Role: "assistant"},
	})
}

func (e *updateEmitter) emitDelta(typ agentkit.AssistantMessageEventType, idx int, delta string) error {
	return e.emitUpdate(agentkit.AssistantMessageEvent{
		Type:         typ,
		ContentIndex: idx,
		Delta:        delta,
	})
}

func (e *updateEmitter) emitUpdate(ame agentkit.AssistantMessageEvent) error {
	return e.sendOutbound(agentkit.EventMessageUpdate, agentkit.MessageUpdatePayload{
		AssistantMessageEvent: ame,
	})
}

func (e *updateEmitter) sendOutbound(typ agentkit.EventType, payload any) error {
	return e.emit(e.ctx, agentkit.OutboundEvent{
		SessionID: e.sessionID,
		AgentID:   e.agentID,
		Type:      typ,
		Data:      agentkit.MarshalOutboundData(payload),
	})
}

func contentText(block acp.ContentBlock) string {
	if block.Text != nil {
		return block.Text.Text
	}
	return ""
}
