package rctx_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestRoutePolicyForPlatformChatAPIUsesDeliveryEntry(t *testing.T) {
	t.Parallel()

	base := rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel)
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
	policy := rctx.RoutePolicyForPlatform(platform, rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
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

func TestAdapterInstancesHaveIndependentLogicalKeys(t *testing.T) {
	t.Parallel()
	route := rctx.SessionRoute("slack", "slack:C1:t:1:u:U1")
	policy := rctx.DefaultRoutePolicy(agentkit.SessionScopeThread)
	resolve := func(instance string) agentkit.TurnEnvelope {
		r := route
		r.Instance = instance
		return rctx.ResolveEnvelope(agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Route: r}}, policy)
	}
	first, second := resolve("slack"), resolve("slack1")
	if first.Conversation != "slack:C1:t:1" || second.Conversation != "slack1:C1:t:1" {
		t.Fatalf("conversations: %q %q", first.Conversation, second.Conversation)
	}
	if first.Workspace != "slack:C1" || second.Workspace != "slack1:C1" {
		t.Fatalf("workspaces: %q %q", first.Workspace, second.Workspace)
	}
	keys := rctx.BindKeysForRoute(second.Route, policy, "U1", agentkit.SessionID(second.Conversation))
	if keys.Channel != "slack1:C1" || keys.Thread != "slack1:C1:t:1" {
		t.Fatalf("bind keys: %+v", keys)
	}
	if id, _ := rctx.RouteSessionID(second.Route); id != "slack:C1:t:1:u:U1" {
		t.Fatalf("platform delivery changed: %q", id)
	}
}
