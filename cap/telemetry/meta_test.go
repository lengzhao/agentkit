package telemetry_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/telemetry"
)

func TestObservationMetaFromContextFillsAgentAndSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("sub:researcher"))
	ctx = context.WithValue(ctx, agentkit.KeySessionID, agentkit.SessionID("sub:parent:researcher:1"))

	meta := telemetry.ObservationMetaFromContext(ctx, telemetry.ObservationMeta{
		Name: "llm.generation",
		Kind: telemetry.KindGeneration,
	})
	if meta.AgentID != "sub:researcher" {
		t.Fatalf("agent_id = %q", meta.AgentID)
	}
	if meta.SessionID != "sub:parent:researcher:1" {
		t.Fatalf("session_id = %q", meta.SessionID)
	}
}

func TestEnrichEventAttrsAddsAgentAndSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("coder"))
	ctx = context.WithValue(ctx, agentkit.KeySessionID, agentkit.SessionID("cli:default"))

	attrs := telemetry.EnrichEventAttrs(ctx, map[string]string{"steps": "2"})
	if attrs["agent_id"] != "coder" {
		t.Fatalf("agent_id = %q", attrs["agent_id"])
	}
	if attrs["session_id"] != "cli:default" {
		t.Fatalf("session_id = %q", attrs["session_id"])
	}
	if attrs["steps"] != "2" {
		t.Fatalf("steps = %q", attrs["steps"])
	}
}
