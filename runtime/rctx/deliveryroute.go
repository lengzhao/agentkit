package rctx

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	delivery "github.com/lengzhao/agentkit/cap/delivery"
)

// ResolveRoute resolves a user-facing destination selector to its stable RouteRef.
func ResolveRoute(ctx context.Context, input delivery.RouteInput) (agentkit.RouteRef, error) {
	current := EnvelopeFromContext(ctx).Route
	raw := strings.TrimSpace(input.SessionID)
	user := strings.TrimSpace(input.UserID)
	if raw == "" && user == "" {
		if id, ok := RouteSessionID(current); ok && id != "" {
			return current.Stable(), nil
		}
		return agentkit.RouteRef{}, fmt.Errorf("requires inbox route in context, or sessionId/userId")
	}
	var id agentkit.SessionID
	protocol, instance := current.Platform, current.DestinationID()
	if raw != "" {
		qualified := strings.Contains(raw, ":")
		id = NormalizeSessionID(ctx, raw)
		prefix := ParseDelivery(id, "").Platform
		if qualified {
			instance = prefix
			if prefix != current.DestinationID() {
				protocol = prefix
			}
		}
		if protocol == "" {
			protocol = prefix
		}
		if prefix == instance && prefix != protocol {
			id = agentkit.SessionID(protocol + strings.TrimPrefix(string(id), prefix))
		}
	} else {
		inbox, _ := RouteSessionID(current)
		id = DeliveryWithUser(inbox, user)
	}
	if id == "" {
		return agentkit.RouteRef{}, fmt.Errorf("requires inbox route in context, or sessionId/userId")
	}
	route := BuildSessionRoute(agentkit.SessionRouteInput{Platform: protocol, Instance: instance, DeliveryID: id})
	return route.Stable(), nil
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

// RouteForPlatform resolves an instance selector into the selected leaf's protocol.
// Transport delivery IDs keep protocol prefixes; Instance retains adapter identity.
func RouteForPlatform(route agentkit.RouteRef, protocol string) (agentkit.RouteRef, error) {
	destination := route.DestinationID()
	if route.Platform != "" && route.Platform != protocol && route.Platform != destination {
		return agentkit.RouteRef{}, fmt.Errorf("route protocol %q does not match %q", route.Platform, protocol)
	}
	route.Platform = protocol
	if target, ok := DecodeSessionRoute(route); ok {
		prefix := ParseDelivery(target.DeliveryID, "").Platform
		if prefix == destination && prefix != protocol {
			target.DeliveryID = agentkit.SessionID(protocol + strings.TrimPrefix(string(target.DeliveryID), prefix))
			route = sessionRouteFromTarget(protocol, target).WithInstance(destination)
		}
	}
	return route, nil
}
