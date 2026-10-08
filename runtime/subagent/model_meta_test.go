package subagent

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
)

type modelStubAgent struct {
	agentkit.AgentID
	model string
}

func (a modelStubAgent) ID() agentkit.AgentID { return a.AgentID }
func (a modelStubAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }
func (a modelStubAgent) ConfiguredModel() string { return a.model }

func TestTelemetryModelPrefersDefinition(t *testing.T) {
	t.Parallel()
	def := capsubagent.Definition{Name: "r", Model: "from-def"}
	ag := modelStubAgent{AgentID: "cursor", model: "from-agent"}
	if got := telemetryModel(def, ag); got != "from-def" {
		t.Fatalf("model = %q, want from-def", got)
	}
}

func TestTelemetryModelFallsBackToAgent(t *testing.T) {
	t.Parallel()
	def := capsubagent.Definition{Name: "r"}
	ag := modelStubAgent{AgentID: "cursor", model: "from-agent"}
	if got := telemetryModel(def, ag); got != "from-agent" {
		t.Fatalf("model = %q, want from-agent", got)
	}
}

func TestInprocessTelemetryModelUsesDefinitionDefault(t *testing.T) {
	t.Parallel()
	def := capsubagent.Definition{Name: "researcher", Model: "from-md"}
	got := inprocessTelemetryModel(context.Background(), nil, nil, "sub:cli:researcher:1", "sub:researcher", def)
	if got != "from-md" {
		t.Fatalf("model = %q, want from-md", got)
	}
}
