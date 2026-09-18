package rctx_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestResolveEnvelopePreservesInboundRoute(t *testing.T) {
	t.Parallel()

	delivery := rctx.BuildDeliverySessionID("slack", "C001", "111.0", "U111")
	route := rctx.SessionRoute("slack", string(delivery))
	event := agentkit.MessageEvent{
		Envelope: agentkit.TurnEnvelope{
			Route: route,
			Actor: agentkit.ActorRef{UserID: "U111"},
		},
		PlatformID: "slack",
		UserID:     "U111",
	}
	policy := rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel)
	env := rctx.ResolveEnvelope(event, policy)
	if id, ok := rctx.RouteSessionID(env.Route); !ok || id != delivery {
		t.Fatalf("route changed: %v", env.Route)
	}
}

func TestResolveEnvelopeSlackChannelScope(t *testing.T) {
	t.Parallel()

	delivery := rctx.BuildDeliverySessionID("slack", "C001", "111.0", "U111")
	event := agentkit.MessageEvent{
		PlatformID: "slack",
		UserID:     "U111",
		Envelope: agentkit.TurnEnvelope{
			Route: rctx.SessionRoute("slack", string(delivery)),
		},
	}
	policy := rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel)
	env := rctx.ResolveEnvelope(event, policy)

	if env.Conversation != "slack:C001" {
		t.Fatalf("conversation = %q", env.Conversation)
	}
	if env.Workspace != "slack:C001" {
		t.Fatalf("workspace = %q", env.Workspace)
	}
	id, ok := rctx.RouteSessionID(env.Route)
	if !ok || id != delivery {
		t.Fatalf("route = %v", env.Route)
	}
}

func TestResolveEnvelopeChatAPIUsesDeliveryConversation(t *testing.T) {
	t.Parallel()

	delivery := rctx.BuildDeliverySessionID("chat-api", "default_channel", "conv_1", "")
	event := agentkit.MessageEvent{
		PlatformID: "chat-api",
		Envelope: agentkit.TurnEnvelope{
			Route: rctx.SessionRoute("chat-api", string(delivery)),
		},
	}
	policy := rctx.RoutePolicyForPlatform("chat-api", rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
	env := rctx.ResolveEnvelope(event, policy)

	if env.Conversation != string(delivery) {
		t.Fatalf("conversation = %q, want delivery", env.Conversation)
	}
	if env.Workspace != "chat-api:default_channel" {
		t.Fatalf("workspace = %q", env.Workspace)
	}
}

func TestEnvelopeWithConversationPreservesRouteAndWorkspace(t *testing.T) {
	t.Parallel()

	env := agentkit.TurnEnvelope{
		Route:        rctx.SessionRoute("slack", "slack:C001:t:1:u:U1"),
		Conversation: "slack:C001",
		Workspace:    "slack:C001",
	}
	next := env.WithConversation("slack:C001:new:20260101")
	if next.Conversation != "slack:C001:new:20260101" {
		t.Fatalf("conversation = %q", next.Conversation)
	}
	if id, _ := rctx.RouteSessionID(next.Route); string(id) != "slack:C001:t:1:u:U1" {
		t.Fatalf("route changed: %v", next.Route)
	}
	if next.Workspace != "slack:C001" {
		t.Fatalf("workspace changed: %q", next.Workspace)
	}
}

func TestChildConversationID(t *testing.T) {
	t.Parallel()

	got := rctx.ChildConversationID("slack:C001", "researcher", 3)
	want := "slack:C001:sub:researcher:3"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestApplyEnvelopeToContextStoresEnvelope(t *testing.T) {
	t.Parallel()

	env := agentkit.TurnEnvelope{
		Route:        rctx.SessionRoute("slack", "slack:C001:t:1:u:U1"),
		Conversation: "slack:C001:new:20260101",
		Workspace:    "slack:C001",
		AgentID:      agentkit.AgentID("coder"),
		Actor:        agentkit.ActorRef{UserID: "U1"},
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), env)

	if got := rctx.SessionIDFromContext(ctx); got != "slack:C001:new:20260101" {
		t.Fatalf("session id = %q", got)
	}
	if got := rctx.AgentIDFromContext(ctx); got != "coder" {
		t.Fatalf("agent id = %q", got)
	}
	if got := rctx.DeliveryRouteFromContext(ctx); got != "slack:C001:t:1:u:U1" {
		t.Fatalf("delivery = %q", got)
	}
	if got := rctx.WorkspaceFromContext(ctx); got != "slack:C001" {
		t.Fatalf("workspace = %q", got)
	}
}

func TestWithRouteUpdatesEnvelopeRoute(t *testing.T) {
	t.Parallel()

	base := agentkit.TurnEnvelope{
		Route:        rctx.SessionRoute("slack", "slack:C001"),
		Conversation: "slack:C001",
		Workspace:    "slack:C001",
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), base)
	ctx = rctx.WithRoute(ctx, rctx.SessionRoute("slack", "slack:C002"))

	if got := rctx.DeliveryRouteFromContext(ctx); got != "slack:C002" {
		t.Fatalf("delivery = %q", got)
	}
	if got := rctx.SessionIDFromContext(ctx); got != "slack:C001" {
		t.Fatalf("conversation unchanged = %q", got)
	}
}

func TestOutboundRouteIDPrefersRoute(t *testing.T) {
	t.Parallel()

	event := agentkit.OutboundEvent{
		Route: rctx.SessionRoute("slack", "slack:C001:t:1"),
	}
	if got := rctx.OutboundRouteID(event); got != "slack:C001:t:1" {
		t.Fatalf("got %q", got)
	}
}
