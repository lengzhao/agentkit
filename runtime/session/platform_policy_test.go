package session_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestRoutePolicyForPlatformChatAPIUsesDeliveryEntry(t *testing.T) {
	t.Parallel()

	base := rctx.DefaultRoutePolicy(session.ScopeChannel)
	policy := rctx.RoutePolicyForPlatform("chat-api", base)
	if policy.ActiveEntryMode != rctx.ActiveEntryDelivery {
		t.Fatalf("active entry = %q", policy.ActiveEntryMode)
	}
}

func TestRegisterPlatformPolicyOverridesDefaults(t *testing.T) {
	t.Parallel()

	const platform = "test-platform-delivery-entry"
	rctx.RegisterPlatformPolicy(platform, rctx.PlatformSessionPolicy{
		ActiveEntryMode: rctx.ActiveEntryDelivery,
	})
	policy := rctx.RoutePolicyForPlatform(platform, rctx.DefaultRoutePolicy(session.ScopeChannel))
	if policy.ActiveEntryMode != rctx.ActiveEntryDelivery {
		t.Fatalf("active entry = %q", policy.ActiveEntryMode)
	}
}

func TestInboundDeliveryIDPrefersRoute(t *testing.T) {
	t.Parallel()

	delivery := rctx.BuildDeliverySessionID("slack", "C001", "1", "U1")
	event := agentkit.MessageEvent{
		Envelope: agentkit.TurnEnvelope{
			Route: rctx.SessionRoute("slack", string(delivery)),
		},
	}
	if got := rctx.InboundDeliveryID(event); got != delivery {
		t.Fatalf("got %q want %q", got, delivery)
	}
}

func TestDeliveryFromEnvelopeUsesRoute(t *testing.T) {
	t.Parallel()

	delivery := agentkit.SessionID("slack:C001:t:1")
	env := agentkit.TurnEnvelope{
		Route: rctx.SessionRoute("slack", string(delivery)),
	}
	if got := rctx.DeliveryFromEnvelope(env); got != delivery {
		t.Fatalf("got %q want %q", got, delivery)
	}
}

func TestDeliveryRouteFromContextPrefersEnvelope(t *testing.T) {
	t.Parallel()

	delivery := rctx.BuildDeliverySessionID("slack", "C001", "1", "U1")
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Route: rctx.SessionRoute("slack", string(delivery)),
	})
	if got := rctx.DeliveryRouteFromContext(ctx); got != delivery {
		t.Fatalf("got %q want %q", got, delivery)
	}
	route := rctx.RouteRefFromContext(ctx)
	if id, ok := rctx.RouteSessionID(route); !ok || id != delivery {
		t.Fatalf("route = %v", route)
	}
}
