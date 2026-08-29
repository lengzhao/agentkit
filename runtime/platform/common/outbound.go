package common

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/lengzhao/agentkit"
)

// TextSender delivers finalized assistant text to a conversation.
type TextSender func(ctx context.Context, sessionID agentkit.SessionID, text string) error

// MediaSender delivers a non-text assistant content part (image or document).
type MediaSender func(ctx context.Context, sessionID agentkit.SessionID, part agentkit.ContentPart) error

// Outbound handles agentkit outbound events with per-session text buffering.
type Outbound struct {
	send  TextSender
	media MediaSender
	mu    sync.Mutex
	buf   map[agentkit.SessionID]string
}

func NewOutbound(send TextSender, media MediaSender) *Outbound {
	return &Outbound{
		send:  send,
		media: media,
		buf:   make(map[agentkit.SessionID]string),
	}
}

func (o *Outbound) Handle(ctx context.Context, event agentkit.OutboundEvent) error {
	switch event.Type {
	case agentkit.EventMessageStart:
		o.clear(event.SessionID)
		return nil
	case agentkit.EventMessageUpdate:
		var payload agentkit.MessageUpdatePayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		switch payload.AssistantMessageEvent.Type {
		case agentkit.AssistantEventTextDelta, agentkit.AssistantEventThinkingDelta:
			o.append(event.SessionID, payload.AssistantMessageEvent.Delta)
		}
		return nil
	case agentkit.EventMessageEnd, agentkit.EventAssistantMessage:
		text := o.take(event.SessionID)
		var msg agentkit.ModelMessage
		if event.Type == agentkit.EventAssistantMessage {
			if err := json.Unmarshal(event.Data, &msg); err == nil {
				if t := textOf(msg); t != "" {
					text = t
				}
			}
		}
		if text != "" {
			if err := o.send(ctx, event.SessionID, text); err != nil {
				return err
			}
		}
		if event.Type == agentkit.EventAssistantMessage && o.media != nil {
			for _, part := range msg.Content {
				if !isOutboundMediaPart(part.Type) {
					continue
				}
				if err := o.media(ctx, event.SessionID, part); err != nil {
					return err
				}
			}
		}
		return nil
	case agentkit.EventPermissionRequest:
		// IM platforms send permission cards in Platform.Send; chat-api handles separately.
		return nil
	case agentkit.EventPermissionResolved, agentkit.EventTurnContinue:
		return nil
	case "error":
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(event.Data, &payload); err == nil && payload.Error != "" {
			return o.send(ctx, event.SessionID, "error: "+payload.Error)
		}
	}
	return nil
}

func (o *Outbound) clear(id agentkit.SessionID) {
	o.mu.Lock()
	delete(o.buf, id)
	o.mu.Unlock()
}

func (o *Outbound) append(id agentkit.SessionID, delta string) {
	if delta == "" {
		return
	}
	o.mu.Lock()
	o.buf[id] += delta
	o.mu.Unlock()
}

func (o *Outbound) take(id agentkit.SessionID) string {
	o.mu.Lock()
	text := o.buf[id]
	delete(o.buf, id)
	o.mu.Unlock()
	return text
}

func isOutboundMediaPart(typ string) bool {
	switch typ {
	case "image", "document":
		return true
	default:
		return false
	}
}

func textOf(msg agentkit.ModelMessage) string {
	var b []byte
	for _, part := range msg.Content {
		if part.Type == "text" && part.Text != "" {
			b = append(b, part.Text...)
		}
	}
	return string(b)
}
