package acpremote

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/loop"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
)

type updateEmitter struct {
	ctx       context.Context
	sessionID agentkit.SessionID
	agentID   agentkit.AgentID
	emit      agentkit.OutboundEmit

	started bool
	textBuf strings.Builder
	thought strings.Builder

	generationCtx context.Context
	endGeneration func(captelemetry.ObservationEnd)
	firstTextAt   time.Time
	tools         map[acp.ToolCallId]func(captelemetry.ObservationEnd)
}

func newUpdateEmitter(ctx context.Context, sessionID agentkit.SessionID, agentID agentkit.AgentID, emit agentkit.OutboundEmit) *updateEmitter {
	return &updateEmitter{
		ctx:       ctx,
		sessionID: sessionID,
		agentID:   agentID,
		emit:      emit,
		tools:     make(map[acp.ToolCallId]func(captelemetry.ObservationEnd)),
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
		e.markFirstText()
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
		e.markFirstText()
		return e.emitDelta(agentkit.AssistantEventThinkingDelta, 1, text)
	case u.ToolCall != nil:
		if err := e.ensureStarted(); err != nil {
			return err
		}
		e.beginTool(u.ToolCall)
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
		e.endTool(u.ToolCallUpdate)
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
	for id, end := range e.tools {
		end(captelemetry.ObservationEnd{
			Err: fmt.Errorf("acp tool call %s ended before completion", id),
		})
		delete(e.tools, id)
	}
	if e.endGeneration != nil {
		e.endGeneration(captelemetry.ObservationEnd{
			Output:              rttelemetry.FormatMessage(msg),
			FirstTextTime:       e.firstTextAt,
			CompletionStartTime: e.firstTextAt,
		})
		e.endGeneration = nil
	}
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
	e.generationCtx, e.endGeneration = rttelemetry.BeginObservation(e.ctx,
		rttelemetry.ObservationMetaFromContext(e.ctx, captelemetry.ObservationMeta{
			Name: "acp.generation",
			Kind: captelemetry.KindGeneration,
		}))
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
		AgentID: e.agentID,
		Type:    typ,
		Data:    loop.MarshalOutboundData(payload),
	})
}

func (e *updateEmitter) markFirstText() {
	if e.firstTextAt.IsZero() {
		e.firstTextAt = time.Now().UTC()
	}
}

func (e *updateEmitter) beginTool(call *acp.SessionUpdateToolCall) {
	if _, ok := e.tools[call.ToolCallId]; ok {
		return
	}
	input := marshalObservationValue(call.RawInput)
	_, end := rttelemetry.BeginObservation(e.generationCtx,
		rttelemetry.ObservationMetaFromContext(e.generationCtx, captelemetry.ObservationMeta{
			Name:  "tool." + toolName(call),
			Kind:  captelemetry.KindTool,
			Input: input,
		}))
	e.tools[call.ToolCallId] = end
}

func toolName(call *acp.SessionUpdateToolCall) string {
	if strings.TrimSpace(call.Title) != "" {
		return strings.TrimSpace(call.Title)
	}
	return string(call.ToolCallId)
}

func (e *updateEmitter) endTool(update *acp.SessionToolCallUpdate) {
	end, ok := e.tools[update.ToolCallId]
	if !ok {
		return
	}
	if update.Status != nil &&
		*update.Status != acp.ToolCallStatusCompleted &&
		*update.Status != acp.ToolCallStatusFailed {
		return
	}
	delete(e.tools, update.ToolCallId)
	observationEnd := captelemetry.ObservationEnd{
		Output: marshalObservationOutput(update.RawOutput, update.Content),
	}
	if update.Status != nil && *update.Status == acp.ToolCallStatusFailed {
		observationEnd.Err = fmt.Errorf("acp tool call failed: %s", update.ToolCallId)
	}
	end(observationEnd)
}

func marshalObservationOutput(raw any, content []acp.ToolCallContent) string {
	if raw != nil {
		return marshalObservationValue(raw)
	}
	if len(content) > 0 {
		return marshalObservationValue(content)
	}
	return ""
}

func marshalObservationValue(value any) string {
	if value == nil {
		return ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(raw)
}

func contentText(block acp.ContentBlock) string {
	if block.Text != nil {
		return block.Text.Text
	}
	return ""
}
