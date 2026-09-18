package chatapi

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

const defaultWorkspaceChannelID = "default_channel"

// engineSessionKey is the delivery SessionID:
// chat-api:<channel>:t:<conversationID>.
// session.WorkspaceKey -> chat-api:<channel> for per-channel workspace isolation.
func engineSessionKey(channelKey, conversationID string) string {
	if channelKey == "" {
		channelKey = defaultWorkspaceChannelID
	}
	return string(rctx.BuildDeliverySessionID(
		"chat-api",
		encodeSessionChannelSegment(channelKey),
		conversationID,
		"",
	))
}

func channelWorkspaceEnvelope(channelKey string) agentkit.TurnEnvelope {
	delivery := engineSessionKey(channelKey, "probe")
	return agentkit.TurnEnvelope{
		Workspace: rctx.WorkspaceKey(delivery),
	}
}

func sessionEnvelope(channelKey, conversationID string) agentkit.TurnEnvelope {
	delivery := agentkit.SessionID(engineSessionKey(channelKey, conversationID))
	return agentkit.TurnEnvelope{
		Conversation: string(delivery),
		Route:        rctx.SessionRouteFromDelivery("chat-api", delivery, ""),
		Workspace:    rctx.WorkspaceKey(string(delivery)),
	}
}

func channelWorkspaceCtx(ctx context.Context, channelKey string) context.Context {
	return rctx.ApplyEnvelopeToContext(ctx, channelWorkspaceEnvelope(channelKey))
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
