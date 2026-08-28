package chatapi

import (
	"strings"

	"github.com/lengzhao/agentkit/runtime/session"
)

const defaultWorkspaceChannelID = "default_channel"

// engineSessionKey is the delivery SessionID:
// chat-api:<channel>:t:<conversationID>.
// tenant.Key -> chat-api:<channel> for per-channel workspace isolation.
func engineSessionKey(channelKey, conversationID string) string {
	if channelKey == "" {
		channelKey = defaultWorkspaceChannelID
	}
	return string(session.BuildDeliverySessionID(
		"chat-api",
		encodeSessionChannelSegment(channelKey),
		conversationID,
		"",
	))
}

func encodeSessionChannelSegment(channelKey string) string {
	return strings.ReplaceAll(channelKey, ":", "%3A")
}

func sessionFilePrefix(channelKey string) string {
	channel := encodeSessionChannelSegment(channelKey)
	if channelKey == "" {
		channel = encodeSessionChannelSegment(defaultWorkspaceChannelID)
	}
	return safeSessionFileSegment("chat-api:" + channel + ":t:")
}
