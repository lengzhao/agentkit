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

func (p *Platform) channelWorkspaceCtx(ctx context.Context, channelKey string) context.Context {
	return rctx.ApplyEnvelopeToContext(ctx, p.sessionEnvelope(channelKey, "probe"))
}
func (p *Platform) sessionEnvelope(channelKey, conversationID string) agentkit.TurnEnvelope {
	route := rctx.SessionRouteFromDelivery("chat-api", agentkit.SessionID(engineSessionKey(channelKey, conversationID)), "").WithInstance(p.PlatformInstance())
	return rctx.ResolveEnvelope(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: route}}, rctx.RoutePolicy{ActiveEntryMode: rctx.ActiveEntryDelivery})
}

func encodeSessionChannelSegment(channelKey string) string {
	return strings.ReplaceAll(channelKey, ":", "%3A")
}

func sessionFilePrefix(channelKey string, instance ...string) string {
	channel := encodeSessionChannelSegment(channelKey)
	if channelKey == "" {
		channel = encodeSessionChannelSegment(defaultWorkspaceChannelID)
	}
	protocol := "chat-api"
	if len(instance) > 0 && instance[0] != "" {
		protocol = instance[0]
	}
	return safeSessionFileSegment(protocol + ":" + channel + ":t:")
}
