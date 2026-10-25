package rctx

import (
	"strconv"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
)

// DefaultCLISessionID is the stable CLI session id used when no /new happened.
const DefaultCLISessionID = agentkit.SessionID("cli:default")

// NewCLISessionID returns a fresh opaque CLI session id.
func NewCLISessionID() agentkit.SessionID {
	return agentkit.SessionID("cli:" + time.Now().UTC().Format("20060102-150405.000"))
}

// NewSessionID returns a fresh logical session id derived from the current
// stable session key. CLI keeps its historic cli:<timestamp> format.
func NewSessionID(base agentkit.SessionID) agentkit.SessionID {
	if strings.HasPrefix(string(base), "cli:") || base == "" {
		return NewCLISessionID()
	}
	return agentkit.SessionID(string(base) + ":new:" + time.Now().UTC().Format("20060102-150405.000"))
}

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
