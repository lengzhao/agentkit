package delivery

import (
	"context"

	"github.com/lengzhao/agentkit"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// Deprecated wrappers: the route helpers live in runtime/rctx so platforms and
// tools can resolve routes without importing this package (and thereby the
// delivery/assistant plugin registration). These wrappers keep the old import
// path working.

// ResolveRoute resolves the delivery session and platform from context.
// Deprecated: use rctx.ResolveRoute.
func ResolveRoute(ctx context.Context, input capsdelivery.RouteInput) (capsdelivery.Route, error) {
	return rctx.ResolveRoute(ctx, input)
}

// NormalizeSessionID maps slash-style targets to delivery session ids.
// Deprecated: use rctx.NormalizeSessionID.
func NormalizeSessionID(ctx context.Context, raw string) agentkit.SessionID {
	return rctx.NormalizeSessionID(ctx, raw)
}

// OutboundRoute builds a session-kind route for proactive delivery.
// Deprecated: use rctx.OutboundRoute.
func OutboundRoute(platform string, deliveryID agentkit.SessionID) agentkit.RouteRef {
	return rctx.OutboundRoute(platform, deliveryID)
}

// OutboundRouteID returns the platform routing target for an outbound event.
func OutboundRouteID(event agentkit.OutboundEvent) agentkit.SessionID {
	return rctx.OutboundRouteID(event)
}
