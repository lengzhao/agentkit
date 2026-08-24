package agent

import (
	"context"

	"github.com/lengzhao/agentkit"
)

type streamEmitter struct {
	ctx       context.Context
	sessionID agentkit.SessionID
	agentID   agentkit.AgentID
	emitFn    agentkit.OutboundEmit

	started     bool
	textStarted bool
	toolStarted map[int]bool
	prevText    string
	prevToolArg map[int]string
	// usage is the latest token accounting the provider reported for this step.
	usage *agentkit.Usage
}

func newStreamEmitter(ctx context.Context, sessionID agentkit.SessionID, agentID agentkit.AgentID, emit agentkit.OutboundEmit) *streamEmitter {
	if emit == nil {
		return nil
	}
	return &streamEmitter{
		ctx:         ctx,
		sessionID:   sessionID,
		agentID:     agentID,
		emitFn:      emit,
		toolStarted: make(map[int]bool),
		prevToolArg: make(map[int]string),
	}
}

func (s *streamEmitter) consume(ev agentkit.LLMEvent) error {
	if s == nil {
		return nil
	}
	if ev.Usage != nil {
		s.usage = ev.Usage
	}
	switch ev.Type {
	case agentkit.AssistantEventStart:
		return s.emitStart(ev.Message)
	case agentkit.AssistantEventTextStart:
		if err := s.ensureStarted(ev.Message); err != nil {
			return err
		}
		return s.emitAssistant(agentkit.AssistantEventTextStart, ev.ContentIndex, "", ev.Message)
	case agentkit.AssistantEventTextDelta:
		if err := s.ensureStarted(ev.Message); err != nil {
			return err
		}
		if err := s.ensureTextStarted(ev.Message); err != nil {
			return err
		}
		delta := ev.Delta
		if delta == "" && ev.Message != nil {
			delta = textDelta(s.prevText, textOf(ev.Message.Content))
		}
		if delta == "" {
			return nil
		}
		s.prevText += delta
		return s.emitAssistant(agentkit.AssistantEventTextDelta, contentIndexOr(ev.ContentIndex, 0), delta, ev.Message)
	case agentkit.AssistantEventTextEnd:
		if err := s.ensureStarted(ev.Message); err != nil {
			return err
		}
		return s.emitAssistant(agentkit.AssistantEventTextEnd, ev.ContentIndex, ev.Delta, ev.Message)
	case agentkit.AssistantEventThinkingStart, agentkit.AssistantEventThinkingDelta, agentkit.AssistantEventThinkingEnd:
		if err := s.ensureStarted(ev.Message); err != nil {
			return err
		}
		return s.emitAssistant(ev.Type, ev.ContentIndex, ev.Delta, ev.Message)
	case agentkit.AssistantEventToolCallStart:
		if err := s.ensureStarted(ev.Message); err != nil {
			return err
		}
		idx := ev.ContentIndex
		s.toolStarted[idx] = true
		ame := agentkit.AssistantMessageEvent{
			Type:         agentkit.AssistantEventToolCallStart,
			ContentIndex: idx,
		}
		if ev.ToolCall != nil {
			ame.ID = string(ev.ToolCall.ID)
			ame.ToolName = ev.ToolCall.Name
			s.prevToolArg[idx] = string(ev.ToolCall.Input)
		}
		return s.emitUpdate(ame, ev.Message)
	case agentkit.AssistantEventToolCallDelta:
		if err := s.ensureStarted(ev.Message); err != nil {
			return err
		}
		idx := ev.ContentIndex
		delta := ev.Delta
		if delta == "" && ev.ToolCall != nil {
			cur := string(ev.ToolCall.Input)
			prev := s.prevToolArg[idx]
			if len(cur) > len(prev) && cur[:len(prev)] == prev {
				delta = cur[len(prev):]
			}
			s.prevToolArg[idx] = cur
		}
		if delta == "" {
			return nil
		}
		return s.emitAssistant(agentkit.AssistantEventToolCallDelta, idx, delta, ev.Message)
	case agentkit.AssistantEventToolCallEnd:
		if err := s.ensureStarted(ev.Message); err != nil {
			return err
		}
		ame := agentkit.AssistantMessageEvent{
			Type:         agentkit.AssistantEventToolCallEnd,
			ContentIndex: ev.ContentIndex,
		}
		if ev.ToolCall != nil {
			call := *ev.ToolCall
			ame.ToolCall = &call
		}
		return s.emitUpdate(ame, ev.Message)
	}
	return nil
}

func (s *streamEmitter) finalize(msg agentkit.ModelMessage) error {
	if s == nil {
		return nil
	}
	if !s.started {
		if err := s.emitStart(&msg); err != nil {
			return err
		}
	}
	return s.sendOutbound(agentkit.EventMessageEnd, agentkit.MessageEndPayload{Message: msg})
}

func (s *streamEmitter) ensureStarted(msg *agentkit.ModelMessage) error {
	if s.started {
		return nil
	}
	return s.emitStart(msg)
}

func (s *streamEmitter) emitStart(msg *agentkit.ModelMessage) error {
	startMsg := agentkit.ModelMessage{Role: "assistant"}
	if msg != nil {
		startMsg = *msg
	}
	if startMsg.Role == "" {
		startMsg.Role = "assistant"
	}
	s.started = true
	return s.sendOutbound(agentkit.EventMessageStart, agentkit.MessageStartPayload{Message: startMsg})
}

func (s *streamEmitter) ensureTextStarted(msg *agentkit.ModelMessage) error {
	if s.textStarted {
		return nil
	}
	s.textStarted = true
	return s.emitAssistant(agentkit.AssistantEventTextStart, 0, "", msg)
}

func (s *streamEmitter) emitAssistant(typ agentkit.AssistantMessageEventType, contentIndex int, delta string, msg *agentkit.ModelMessage) error {
	ame := agentkit.AssistantMessageEvent{
		Type:         typ,
		ContentIndex: contentIndex,
		Delta:        delta,
	}
	if typ == agentkit.AssistantEventTextEnd {
		ame.Content = delta
		ame.Delta = ""
	}
	return s.emitUpdate(ame, msg)
}

func (s *streamEmitter) emitUpdate(ame agentkit.AssistantMessageEvent, _ *agentkit.ModelMessage) error {
	payload := agentkit.MessageUpdatePayload{AssistantMessageEvent: ame, Usage: s.usage}
	return s.sendOutbound(agentkit.EventMessageUpdate, payload)
}

func (s *streamEmitter) sendOutbound(typ agentkit.EventType, payload any) error {
	return s.emitFn(s.ctx, agentkit.OutboundEvent{
		SessionID: s.sessionID,
		AgentID:   s.agentID,
		Type:      typ,
		Data:      agentkit.MarshalOutboundData(payload),
	})
}

func textDelta(prev, cur string) string {
	if len(cur) <= len(prev) {
		return ""
	}
	if cur[:len(prev)] != prev {
		return cur
	}
	return cur[len(prev):]
}

func contentIndexOr(idx, fallback int) int {
	if idx != 0 {
		return idx
	}
	return fallback
}

func textOf(parts []agentkit.ContentPart) string {
	var b []byte
	for _, part := range parts {
		if part.Type == "text" {
			b = append(b, part.Text...)
		}
	}
	return string(b)
}
