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
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type updateEmitter struct {
	telemetry captelemetry.Toolkit
	ctx       context.Context
	sessionID agentkit.SessionID
	agentID   agentkit.AgentID
	emit      agentkit.OutboundEmit
	userMsg   agentkit.ModelMessage

	started bool
	textBuf strings.Builder
	thought strings.Builder

	generationCtx context.Context
	endGeneration func(captelemetry.ObservationEnd)
	firstTextAt   time.Time
	tools         map[acp.ToolCallId]func(captelemetry.ObservationEnd)
	toolMeta      map[acp.ToolCallId]acpToolMeta
}

type acpToolMeta struct {
	name  string
	input string
}

func newUpdateEmitter(telemetry captelemetry.Toolkit, ctx context.Context, sessionID agentkit.SessionID, agentID agentkit.AgentID, emit agentkit.OutboundEmit, userMsg agentkit.ModelMessage) *updateEmitter {
	return &updateEmitter{
		telemetry: telemetry,
		ctx:       ctx,
		sessionID: sessionID,
		agentID:   agentID,
		emit:      emit,
		userMsg:   userMsg,
		tools:     make(map[acp.ToolCallId]func(captelemetry.ObservationEnd)),
		toolMeta:  make(map[acp.ToolCallId]acpToolMeta),
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
		meta := e.toolMeta[u.ToolCallUpdate.ToolCallId]
		name, output, failed, terminal := e.completeToolUpdate(u.ToolCallUpdate)
		if !terminal {
			return nil
		}
		ame := agentkit.AssistantMessageEvent{
			Type:         agentkit.AssistantEventToolCallEnd,
			ContentIndex: 2,
			ID:           string(u.ToolCallUpdate.ToolCallId),
		}
		if meta.input != "" {
			toolName := meta.name
			if toolName == "" {
				toolName = name
			}
			ame.ToolCall = &agentkit.ToolCall{
				ID:    agentkit.ToolCallID(u.ToolCallUpdate.ToolCallId),
				Name:  toolName,
				Input: json.RawMessage(meta.input),
			}
		} else if name != "" {
			ame.ToolName = name
		}
		if err := e.emitUpdate(ame); err != nil {
			return err
		}
		return e.emitToolResult(u.ToolCallUpdate.ToolCallId, name, output, failed)
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
		delete(e.toolMeta, id)
	}
	if e.endGeneration != nil {
		e.endGeneration(captelemetry.ObservationEnd{
			Output:              e.telemetry.FormatMessage(msg),
			FirstTextTime:       e.firstTextAt,
			CompletionStartTime: e.firstTextAt,
		})
		e.endGeneration = nil
	}
	return e.sendOutbound(agentkit.EventMessageEnd, agentkit.MessageEndPayload{Message: msg})
}

func (e *updateEmitter) assistantMessage() agentkit.ModelMessage {
	msg := agentkit.ModelMessage{Role: "assistant"}
	var parts []agentkit.ContentPart
	if e.thought.Len() > 0 {
		parts = append(parts, agentkit.ContentPart{Type: "thinking", Text: e.thought.String()})
	}
	if e.textBuf.Len() > 0 {
		parts = append(parts, agentkit.ContentPart{Type: "text", Text: e.textBuf.String()})
	}
	msg.Content = parts
	return msg
}

func (e *updateEmitter) ensureStarted() error {
	if e.started {
		return nil
	}
	e.started = true
	genMeta := captelemetry.ObservationMeta{
		Name: "acp.generation",
		Kind: captelemetry.KindGeneration,
		Attributes: map[string]string{
			"observation_kind": "generation",
			"acp_agent":        string(e.agentID),
		},
	}
	if e.userMsg.Role != "" || len(e.userMsg.Content) > 0 || len(e.userMsg.ToolCalls) > 0 {
		genMeta.GenerationMessages = []agentkit.ModelMessage{e.userMsg}
		genMeta.Input = e.telemetry.FormatMessage(e.userMsg)
	}
	e.generationCtx, e.endGeneration = e.telemetry.BeginObservation(e.ctx,
		e.telemetry.ObservationMetaFromContext(e.ctx, genMeta))
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
		Data:    rctx.MarshalOutboundData(payload),
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
	e.toolMeta[call.ToolCallId] = acpToolMeta{
		name:  toolName(call),
		input: input,
	}
	_, end := e.telemetry.BeginObservation(e.generationCtx,
		e.telemetry.ObservationMetaFromContext(e.generationCtx, captelemetry.ObservationMeta{
			Name:  "tool." + toolName(call),
			Kind:  captelemetry.KindTool,
			Input: input,
		}))
	e.tools[call.ToolCallId] = end
}

func (e *updateEmitter) completeToolUpdate(update *acp.SessionToolCallUpdate) (name, output string, failed, terminal bool) {
	end, ok := e.tools[update.ToolCallId]
	if !ok {
		return "", "", false, false
	}
	if update.Status != nil &&
		*update.Status != acp.ToolCallStatusCompleted &&
		*update.Status != acp.ToolCallStatusFailed {
		return "", "", false, false
	}
	meta := e.toolMeta[update.ToolCallId]
	delete(e.tools, update.ToolCallId)
	delete(e.toolMeta, update.ToolCallId)

	name = meta.name
	if name == "" {
		name = string(update.ToolCallId)
	}
	output = marshalObservationOutput(update.RawOutput, update.Content)
	failed = update.Status != nil && *update.Status == acp.ToolCallStatusFailed
	terminal = true

	observationEnd := captelemetry.ObservationEnd{Output: output}
	if failed {
		observationEnd.Err = fmt.Errorf("acp tool call failed: %s", update.ToolCallId)
	}
	end(observationEnd)
	return name, output, failed, terminal
}

func (e *updateEmitter) emitToolResult(id acp.ToolCallId, name, content string, failed bool) error {
	if name == "" {
		name = string(id)
	}
	result := agentkit.ToolResult{
		ID:      agentkit.ToolCallID(id),
		Name:    name,
		Content: content,
	}
	if failed {
		result.Audit = map[string]string{"status": "failed"}
	}
	return e.sendOutbound(agentkit.EventToolResult, result)
}

func toolName(call *acp.SessionUpdateToolCall) string {
	if strings.TrimSpace(call.Title) != "" {
		return strings.TrimSpace(call.Title)
	}
	return string(call.ToolCallId)
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
