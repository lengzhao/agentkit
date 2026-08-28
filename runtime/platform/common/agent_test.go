package common

import (
	"testing"

	"github.com/lengzhao/agentkit"
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
	if event.AgentID != "coder" {
		t.Fatalf("AgentID = %q, want coder", event.AgentID)
	}
	if event.SessionID != "s1" || event.PlatformID != "slack" || event.UserID != "u1" {
		t.Fatalf("unexpected routing fields: %+v", event)
	}
}
