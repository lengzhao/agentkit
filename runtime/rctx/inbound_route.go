package rctx

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// WithInboundRoute attaches a structured session route to an inbound event.
func WithInboundRoute(event agentkit.MessageEvent, route agentkit.SessionRouteInput) agentkit.MessageEvent {
	return WithDeliveryRoute(event, BuildSessionRoute(route))
}

// WithDeliveryRoute attaches the platform return address to an inbound event.
// Runner resolves conversation and workspace before dispatch.
func WithDeliveryRoute(event agentkit.MessageEvent, route agentkit.RouteRef) agentkit.MessageEvent {
	if route.Platform != "" || route.HasTarget() || route.Instance != "" {
		if route.Instance == "" {
			route.Instance = event.Envelope.Route.Instance
		}
		if route.Instance == "" {
			route.Instance = route.Platform
		}
		event.Envelope.Route = route
	}
	return event
}

// WithDeliverySession builds a session-kind route from platform and delivery ids.
// Prefer WithInboundRoute when channel, thread, or ReplyTo are known.
func WithDeliverySession(event agentkit.MessageEvent, platformID string, delivery agentkit.SessionID) agentkit.MessageEvent {
	platformID = strings.TrimSpace(platformID)
	delivery = agentkit.SessionID(strings.TrimSpace(string(delivery)))
	if delivery == "" {
		return event
	}
	return WithInboundRoute(event, agentkit.SessionRouteInput{
		Platform:   platformID,
		DeliveryID: delivery,
	})
}
