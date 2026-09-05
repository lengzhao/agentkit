package common

import (
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// WithInboundRoute attaches a structured session route to an inbound event.
func WithInboundRoute(event agentkit.MessageEvent, route session.SessionRouteInput) agentkit.MessageEvent {
	return WithDeliveryRoute(event, session.BuildSessionRoute(route))
}

// WithDeliveryRoute attaches the platform return address to an inbound event.
// Runner SyncMessageEvent copies the resolved envelope onto the inbound event.
func WithDeliveryRoute(event agentkit.MessageEvent, route agentkit.RouteRef) agentkit.MessageEvent {
	if route.Platform != "" || route.HasTarget() {
		event.Envelope.Route = route
		if event.PlatformID == "" && route.Platform != "" {
			event.PlatformID = strings.TrimSpace(route.Platform)
		}
	}
	if uid := strings.TrimSpace(event.UserID); uid != "" && event.Envelope.Actor.UserID == "" {
		event.Envelope.Actor.UserID = uid
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
	return WithInboundRoute(event, session.SessionRouteInput{
		Platform:   platformID,
		DeliveryID: delivery,
	})
}
