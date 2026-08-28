package telemetry_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/telemetry"
)

func TestSubagentObservationsNestUnderDelegate(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	ctx := telemetry.WithExporter(context.Background(), rec)
	ctx, endTurn := rec.BeginTurn(ctx, telemetry.TurnMeta{
		TurnID:    "turn-1",
		SessionID: "cli:default",
		AgentID:   "coder",
	})
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("coder"))
	ctx = context.WithValue(ctx, agentkit.KeySessionID, agentkit.SessionID("cli:default"))

	ctx, endParentGen := rec.BeginObservation(ctx, telemetry.ObservationMeta{
		Name:    "llm.generation",
		Kind:    telemetry.KindGeneration,
		AgentID: "coder",
	})
	endParentGen(telemetry.ObservationEnd{Output: "call delegate"})

	ctx, endDelegate := rec.BeginObservation(ctx, telemetry.ObservationMeta{
		Name:  "tool.delegate",
		Kind:  telemetry.KindTool,
		Input: `{"agent":"researcher"}`,
	})

	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("sub:researcher"))
	ctx = context.WithValue(ctx, agentkit.KeySessionID, agentkit.SessionID("sub:cli:researcher:1"))
	ctx, endSubagent := rec.BeginObservation(ctx, telemetry.ObservationMetaFromContext(ctx, telemetry.ObservationMeta{
		Name:  "subagent.researcher",
		Kind:  telemetry.KindSpan,
		Input: "research task",
		Scope: true,
	}))

	ctx, endChildGen := rec.BeginObservation(ctx, telemetry.ObservationMetaFromContext(ctx, telemetry.ObservationMeta{
		Name: "llm.generation",
		Kind: telemetry.KindGeneration,
	}))
	endChildGen(telemetry.ObservationEnd{Output: "child answer"})

	endSubagent(telemetry.ObservationEnd{Output: "child answer"})
	endDelegate(telemetry.ObservationEnd{Output: "child answer"})
	endTurn(telemetry.TurnEnd{})

	_, observations, _ := rec.Snapshot()
	if len(observations) != 4 {
		t.Fatalf("observations = %d, want 4", len(observations))
	}

	byName := make(map[string]telemetry.RecordedObservation, len(observations))
	for _, obs := range observations {
		byName[obs.Meta.Name] = obs
	}
	var parentGen telemetry.RecordedObservation
	var childObs telemetry.RecordedObservation
	for _, obs := range observations {
		switch obs.Meta.Name {
		case "llm.generation":
			if obs.Meta.AgentID == "sub:researcher" {
				childObs = obs
			} else {
				parentGen = obs
			}
		}
	}
	delegate := byName["tool.delegate"]
	subagent := byName["subagent.researcher"]
	if parentGen.ID == "" {
		t.Fatal("missing parent generation observation")
	}
	if childObs.ID == "" {
		t.Fatal("missing child generation observation")
	}

	if parentGen.ParentID != "" {
		t.Fatalf("parent generation parent = %q, want root", parentGen.ParentID)
	}
	if delegate.ParentID != parentGen.ID {
		t.Fatalf("delegate parent = %q, want %q", delegate.ParentID, parentGen.ID)
	}
	if subagent.ParentID != delegate.ID {
		t.Fatalf("subagent parent = %q, want %q", subagent.ParentID, delegate.ID)
	}
	if childObs.ParentID != subagent.ID {
		t.Fatalf("child generation parent = %q, want %q", childObs.ParentID, subagent.ID)
	}
	if childObs.Meta.AgentID != "sub:researcher" {
		t.Fatalf("child agent_id = %q", childObs.Meta.AgentID)
	}
}
