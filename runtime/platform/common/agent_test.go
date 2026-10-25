package common

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestResolveAgentID(t *testing.T) {
	if got := (AgentRoutingConfig{AgentID: " coder "}).ResolveAgentID(); got != "coder" {
		t.Fatalf("ResolveAgentID() = %q, want coder", got)
	}
	if got := (AgentRoutingConfig{}).ResolveAgentID(); got != "" {
		t.Fatalf("empty ResolveAgentID() = %q, want empty", got)
	}
}

func TestInboundMessageSetsAgentID(t *testing.T) {
	event := InboundMessage("coder", agentkit.SessionID("s1"), "slack", "u1", "hi")
	if event.Envelope.AgentID != "coder" {
		t.Fatalf("AgentID = %q, want coder", event.Envelope.AgentID)
	}
	delivery, ok := rctx.RouteSessionID(event.Envelope.Route)
	if !ok || delivery != "s1" || event.Envelope.Route.Instance != "slack" || event.Envelope.Actor.UserID != "u1" {
		t.Fatalf("unexpected routing fields: %+v", event)
	}
}
