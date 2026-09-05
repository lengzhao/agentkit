package session_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestResolveEnvelopePreservesInboundRoute(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("slack", "C001", "111.0", "U111")
	route := agentkit.SessionRoute("slack", string(delivery))
	event := agentkit.MessageEvent{
		Envelope: agentkit.TurnEnvelope{
			Route: route,
			Actor: agentkit.ActorRef{UserID: "U111"},
		},
		PlatformID: "slack",
		UserID:     "U111",
	}
	policy := session.DefaultRoutePolicy(session.ScopeChannel)
	env := session.ResolveEnvelope(event, policy)
	if id, ok := session.RouteSessionID(env.Route); !ok || id != delivery {
		t.Fatalf("route changed: %v", env.Route)
	}
}

func TestResolveEnvelopeSlackChannelScope(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("slack", "C001", "111.0", "U111")
	event := agentkit.MessageEvent{
		PlatformID: "slack",
		UserID:     "U111",
		Envelope: agentkit.TurnEnvelope{
			Route: agentkit.SessionRoute("slack", string(delivery)),
		},
	}
	policy := session.DefaultRoutePolicy(session.ScopeChannel)
	env := session.ResolveEnvelope(event, policy)

	if env.Conversation != "slack:C001" {
		t.Fatalf("conversation = %q", env.Conversation)
	}
	if env.Workspace != "slack:C001" {
		t.Fatalf("workspace = %q", env.Workspace)
	}
	id, ok := session.RouteSessionID(env.Route)
	if !ok || id != delivery {
		t.Fatalf("route = %v", env.Route)
	}
}

func TestResolveEnvelopeChatAPIUsesDeliveryConversation(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("chat-api", "default_channel", "conv_1", "")
	event := agentkit.MessageEvent{
		PlatformID: "chat-api",
		Envelope: agentkit.TurnEnvelope{
			Route: agentkit.SessionRoute("chat-api", string(delivery)),
		},
	}
	policy := session.RoutePolicyForPlatform("chat-api", session.DefaultRoutePolicy(session.ScopeChannel))
	env := session.ResolveEnvelope(event, policy)

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
		Route:        agentkit.SessionRoute("slack", "slack:C001:t:1:u:U1"),
		Conversation: "slack:C001",
		Workspace:    "slack:C001",
	}
	next := env.WithConversation("slack:C001:new:20260101")
	if next.Conversation != "slack:C001:new:20260101" {
		t.Fatalf("conversation = %q", next.Conversation)
	}
	if id, _ := session.RouteSessionID(next.Route); string(id) != "slack:C001:t:1:u:U1" {
		t.Fatalf("route changed: %v", next.Route)
	}
	if next.Workspace != "slack:C001" {
		t.Fatalf("workspace changed: %q", next.Workspace)
	}
}

func TestChildConversationID(t *testing.T) {
	t.Parallel()

	got := session.ChildConversationID("slack:C001", "researcher", 3)
	want := "slack:C001:sub:researcher:3"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestApplyEnvelopeToContextStoresEnvelope(t *testing.T) {
	t.Parallel()

	env := agentkit.TurnEnvelope{
		Route:        agentkit.SessionRoute("slack", "slack:C001:t:1:u:U1"),
		Conversation: "slack:C001:new:20260101",
		Workspace:    "slack:C001",
		AgentID:      agentkit.AgentID("coder"),
		Actor:        agentkit.ActorRef{UserID: "U1"},
	}
	ctx := session.ApplyEnvelopeToContext(context.Background(), env)

	if got := session.SessionIDFromContext(ctx); got != "slack:C001:new:20260101" {
		t.Fatalf("session id = %q", got)
	}
	if got := session.AgentIDFromContext(ctx); got != "coder" {
		t.Fatalf("agent id = %q", got)
	}
	if got := session.DeliveryRouteFromContext(ctx); got != "slack:C001:t:1:u:U1" {
		t.Fatalf("delivery = %q", got)
	}
	if got := session.WorkspaceFromContext(ctx); got != "slack:C001" {
		t.Fatalf("workspace = %q", got)
	}
}

func TestWithRouteUpdatesEnvelopeRoute(t *testing.T) {
	t.Parallel()

	base := agentkit.TurnEnvelope{
		Route:        agentkit.SessionRoute("slack", "slack:C001"),
		Conversation: "slack:C001",
		Workspace:    "slack:C001",
	}
	ctx := session.ApplyEnvelopeToContext(context.Background(), base)
	ctx = session.WithRoute(ctx, agentkit.SessionRoute("slack", "slack:C002"))

	if got := session.DeliveryRouteFromContext(ctx); got != "slack:C002" {
		t.Fatalf("delivery = %q", got)
	}
	if got := session.SessionIDFromContext(ctx); got != "slack:C001" {
		t.Fatalf("conversation unchanged = %q", got)
	}
}

func TestOutboundRouteIDPrefersRoute(t *testing.T) {
	t.Parallel()

	event := agentkit.OutboundEvent{
		Route: agentkit.SessionRoute("slack", "slack:C001:t:1"),
	}
	if got := session.OutboundRouteID(event); got != "slack:C001:t:1" {
		t.Fatalf("got %q", got)
	}
}
