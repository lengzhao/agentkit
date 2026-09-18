package rctx

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// ConversationFromEvent returns the conversation key for Loop locking and history.
func ConversationFromEvent(event agentkit.MessageEvent) agentkit.SessionID {
	if c := strings.TrimSpace(event.Envelope.Conversation); c != "" {
		return agentkit.SessionID(c)
	}
	return ""
}

// ConversationFromLoopRequest returns the conversation key for a queued turn.
func ConversationFromLoopRequest(req agentkit.LoopRequest) agentkit.SessionID {
	return ConversationFromEvent(req.Event)
}

// ChannelKeyMatches reports whether a job belongs to the given channel key.
func ChannelKeyMatches(jobChannelKey, contextChannelKey string) bool {
	return strings.TrimSpace(jobChannelKey) == strings.TrimSpace(contextChannelKey)
}
