package session

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// InboundDeliveryID returns the platform delivery target from an inbound message.
func InboundDeliveryID(event agentkit.MessageEvent) agentkit.SessionID {
	return DeliveryFromEnvelope(event.Envelope)
}

// DeliveryFromEnvelope returns the outbound delivery id from a turn envelope.
func DeliveryFromEnvelope(env agentkit.TurnEnvelope) agentkit.SessionID {
	id, _ := RouteSessionID(env.Route)
	return id
}

// DeliveryRouteFromContext returns the platform delivery target from the turn envelope.
func DeliveryRouteFromContext(ctx context.Context) agentkit.SessionID {
	if id, ok := RouteSessionID(EnvelopeFromContext(ctx).Route); ok && id != "" {
		return id
	}
	return ""
}

// RouteRefFromContext returns the outbound route from the turn envelope.
func RouteRefFromContext(ctx context.Context) agentkit.RouteRef {
	return EnvelopeFromContext(ctx).Route
}

// ContextWithDeliveryRoute attaches a delivery route to ctx.
func ContextWithDeliveryRoute(ctx context.Context, platform string, delivery agentkit.SessionID) context.Context {
	return ApplyEnvelopeToContext(ctx, agentkit.TurnEnvelope{
		Route: SessionRouteFromDelivery(platform, delivery, ""),
	})
}
