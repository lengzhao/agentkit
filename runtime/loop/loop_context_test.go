package loop

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func testEnvelope(route, conversation, workspace, userID string) agentkit.TurnEnvelope {
	return agentkit.TurnEnvelope{
		Route:        agentkit.SessionRoute("slack", route),
		Conversation: conversation,
		Workspace:    workspace,
		Actor:        agentkit.ActorRef{UserID: userID},
	}
}

func TestWithTurnContextSetsUserID(t *testing.T) {
	t.Parallel()

	ctx := withTurnContext(
		context.Background(),
		testEnvelope("slack:C001:t:111.0:u:U456", "slack:C001", "slack:C001", "U456"),
		agentkit.SessionID("slack:C001"),
		agentkit.AgentID("coder"),
		"slack",
		"U456",
		nil,
		nil,
		nil,
	)

	userID := session.UserIDFromContext(ctx)
	if userID != "U456" {
		t.Fatalf("user id = %q", userID)
	}
}

func TestWithTurnContextOmitsEmptyUserID(t *testing.T) {
	t.Parallel()

	ctx := withTurnContext(
		context.Background(),
		agentkit.TurnEnvelope{
			Route:        agentkit.SessionRoute("cli", "cli:default"),
			Conversation: "cli:default",
			Workspace:    "cli:default",
		},
		agentkit.SessionID("cli:default"),
		agentkit.AgentID("coder"),
		"cli",
		"",
		nil,
		nil,
		nil,
	)

	if session.UserIDFromContext(ctx) != "" {
		t.Fatal("expected no user id in context")
	}
}

func TestWithTurnContextSetsDeliveryRoute(t *testing.T) {
	t.Parallel()

	ctx := withTurnContext(
		context.Background(),
		testEnvelope("slack:C001:t:111.0:u:U456", "slack:C001", "slack:C001", "U456"),
		agentkit.SessionID("slack:C001"),
		agentkit.AgentID("coder"),
		"slack",
		"U456",
		nil,
		nil,
		nil,
	)

	delivery := session.DeliveryRouteFromContext(ctx)
	if delivery != "slack:C001:t:111.0:u:U456" {
		t.Fatalf("delivery session id = %q", delivery)
	}
	if session.SessionIDFromContext(ctx) != "slack:C001" {
		t.Fatalf("effective session id = %q", session.SessionIDFromContext(ctx))
	}
}

func TestWithTurnContextSetsResolvedSessionID(t *testing.T) {
	t.Parallel()

	ctx := withTurnContext(
		context.Background(),
		testEnvelope("slack:C001:t:111.0:u:U456", "slack:C001:new:20260829", "slack:C001", "U456"),
		agentkit.SessionID("slack:C001:new:20260829"),
		agentkit.AgentID("coder"),
		"slack",
		"U456",
		nil,
		nil,
		nil,
	)

	if session.SessionIDFromContext(ctx) != "slack:C001:new:20260829" {
		t.Fatalf("session id = %q", session.SessionIDFromContext(ctx))
	}
	delivery := session.DeliveryRouteFromContext(ctx)
	if delivery != "slack:C001:t:111.0:u:U456" {
		t.Fatalf("delivery session id = %q", delivery)
	}
}

func TestWithTurnContextSetsEnvelopeWorkspace(t *testing.T) {
	t.Parallel()

	ctx := withTurnContext(
		context.Background(),
		testEnvelope("slack:C001:t:111.0:u:U456", "slack:C001", "slack:C001", "U456"),
		agentkit.SessionID("slack:C001"),
		agentkit.AgentID("coder"),
		"slack",
		"U456",
		nil,
		nil,
		nil,
	)

	env, ok := ctx.Value(agentkit.KeyTurnEnvelope).(agentkit.TurnEnvelope)
	if !ok || env.Workspace != "slack:C001" {
		t.Fatalf("envelope workspace = %q, ok = %v", env.Workspace, ok)
	}
}
