package session_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestRoutePolicyForPlatformChatAPIUsesDeliveryEntry(t *testing.T) {
	t.Parallel()

	base := session.DefaultRoutePolicy(session.ScopeChannel)
	policy := session.RoutePolicyForPlatform("chat-api", base)
	if policy.ActiveEntryMode != session.ActiveEntryDelivery {
		t.Fatalf("active entry = %q", policy.ActiveEntryMode)
	}
}

func TestRegisterPlatformPolicyOverridesDefaults(t *testing.T) {
	t.Parallel()

	const platform = "test-platform-delivery-entry"
	session.RegisterPlatformPolicy(platform, session.PlatformSessionPolicy{
		ActiveEntryMode: session.ActiveEntryDelivery,
	})
	policy := session.RoutePolicyForPlatform(platform, session.DefaultRoutePolicy(session.ScopeChannel))
	if policy.ActiveEntryMode != session.ActiveEntryDelivery {
		t.Fatalf("active entry = %q", policy.ActiveEntryMode)
	}
}

func TestInboundDeliveryIDPrefersRoute(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("slack", "C001", "1", "U1")
	event := agentkit.MessageEvent{
		Envelope: agentkit.TurnEnvelope{
			Route: agentkit.SessionRoute("slack", string(delivery)),
		},
	}
	if got := session.InboundDeliveryID(event); got != delivery {
		t.Fatalf("got %q want %q", got, delivery)
	}
}

func TestDeliveryFromEnvelopeUsesRoute(t *testing.T) {
	t.Parallel()

	delivery := agentkit.SessionID("slack:C001:t:1")
	env := agentkit.TurnEnvelope{
		Route: agentkit.SessionRoute("slack", string(delivery)),
	}
	if got := session.DeliveryFromEnvelope(env); got != delivery {
		t.Fatalf("got %q want %q", got, delivery)
	}
}

func TestDeliveryRouteFromContextPrefersEnvelope(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("slack", "C001", "1", "U1")
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Route: agentkit.SessionRoute("slack", string(delivery)),
	})
	if got := session.DeliveryRouteFromContext(ctx); got != delivery {
		t.Fatalf("got %q want %q", got, delivery)
	}
	route := session.RouteRefFromContext(ctx)
	if id, ok := session.RouteSessionID(route); !ok || id != delivery {
		t.Fatalf("route = %v", route)
	}
}
