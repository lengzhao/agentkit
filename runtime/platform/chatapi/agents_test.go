package chatapi

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
)

type stubAgent struct{ id agentkit.AgentID }

func (s stubAgent) ID() agentkit.AgentID { return s.id }
func (s stubAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }

func TestCollectAgentIDs(t *testing.T) {
	got := collectAgentIDs([]string{" reviewer ", "coder"}, []agentkit.Agent{
		stubAgent{id: "coder"},
		stubAgent{id: "planner"},
	})
	want := []string{"coder", "planner", "reviewer"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestResolveInboundAgentID(t *testing.T) {
	p := &Platform{agentID: "platform"}
	conv := &conversation{AgentID: "session"}

	if got := p.resolveInboundAgentID("request", conv); got != "request" {
		t.Fatalf("override = %q", got)
	}
	if got := p.resolveInboundAgentID("", conv); got != "session" {
		t.Fatalf("conversation = %q", got)
	}
	if got := p.resolveInboundAgentID("", &conversation{}); got != "platform" {
		t.Fatalf("platform = %q", got)
	}
}

func TestValidateAgentID(t *testing.T) {
	p := &Platform{availableAgents: []string{"coder", "reviewer"}}
	if err := p.validateAgentID("coder"); err != nil {
		t.Fatal(err)
	}
	if err := p.validateAgentID(""); err != nil {
		t.Fatal(err)
	}
	if err := p.validateAgentID("missing"); err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestConversationBindAgent(t *testing.T) {
	c := &conversation{}
	c.bindAgent(" coder ")
	if c.agentID() != "coder" {
		t.Fatalf("agent = %q", c.agentID())
	}
	c.bindAgent("")
	if c.agentID() != "coder" {
		t.Fatalf("empty bind should keep agent, got %q", c.agentID())
	}
}

func TestConversationViewIncludesAgentID(t *testing.T) {
	view := toConversationView(&conversation{
		ID:      "conv_1",
		AgentID: "coder",
	})
	if view["agent_id"] != "coder" {
		t.Fatalf("agent_id = %v", view["agent_id"])
	}
}
