package rctx

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	delivery "github.com/lengzhao/agentkit/cap/delivery"
)

// ResolveRoute resolves the delivery session and platform from context.
func ResolveRoute(ctx context.Context, input delivery.RouteInput) (delivery.Route, error) {
	inbox := DeliveryRouteFromContext(ctx)

	var r delivery.Route
	switch {
	case strings.TrimSpace(input.SessionID) != "":
		r.SessionID = NormalizeSessionID(ctx, input.SessionID)
	case strings.TrimSpace(input.UserID) != "":
		r.SessionID = DeliveryWithUser(inbox, input.UserID)
	default:
		r.SessionID = inbox
	}
	if r.SessionID == "" {
		return delivery.Route{}, fmt.Errorf("requires inbox session in context, or sessionId/userId")
	}
	r.AgentID = AgentIDFromContext(ctx)
	r.PlatformID = PlatformFromContext(ctx)
	if id := strings.TrimSpace(input.UserID); id != "" {
		r.UserID = id
	} else {
		r.UserID = UserIDFromContext(ctx)
	}
	if p := ParseDelivery(r.SessionID, r.UserID).Platform; p != "" {
		if r.PlatformID == "" || strings.TrimSpace(input.SessionID) != "" {
			r.PlatformID = p
		}
	}
	return r, nil
}

// NormalizeSessionID maps slash-style targets to delivery session ids.
// Bare ids without ":" are qualified with the inbox platform from context.
func NormalizeSessionID(ctx context.Context, raw string) agentkit.SessionID {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ":") {
		return agentkit.SessionID(raw)
	}
	if platformID := PlatformFromContext(ctx); platformID != "" {
		return agentkit.SessionID(platformID + ":" + raw)
	}
	return agentkit.SessionID(raw)
}

// OutboundRoute builds a session-kind route for proactive delivery.
func OutboundRoute(platform string, deliveryID agentkit.SessionID) agentkit.RouteRef {
	return SessionRouteFromDelivery(platform, deliveryID, "")
}
