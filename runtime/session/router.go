package session

import (
	"strconv"

	"github.com/lengzhao/agentkit"
)

// NewConversationID returns a fresh conversation id for /new.
func NewConversationID(current string) string {
	return string(NewSessionID(agentkit.SessionID(current)))
}

// ChildConversationID returns a subagent conversation id under a parent.
func ChildConversationID(parentConversation, agentName string, seq int64) string {
	return parentConversation + ":sub:" + agentName + ":" + strconv.FormatInt(seq, 10)
}

// SyncMessageEvent copies resolved envelope fields onto an inbound message.
func SyncMessageEvent(event agentkit.MessageEvent, env agentkit.TurnEnvelope) agentkit.MessageEvent {
	event.Envelope = env
	if event.PlatformID == "" {
		event.PlatformID = env.Route.Platform
	}
	if event.UserID == "" {
		event.UserID = env.Actor.UserID
	}
	if len(event.Metadata) == 0 && len(env.Metadata) != 0 {
		event.Metadata = env.Metadata
	}
	return event
}

// OutboundFromEnvelope builds an outbound event from envelope routing context.
func OutboundFromEnvelope(env agentkit.TurnEnvelope, typ agentkit.EventType, data []byte) agentkit.OutboundEvent {
	return agentkit.OutboundEvent{
		Route:      env.Route,
		PlatformID: env.Route.Platform,
		UserID:     env.Actor.UserID,
		Type:       typ,
		Data:       data,
	}
}
