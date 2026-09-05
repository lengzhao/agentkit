package command

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/session"
)

type stubAgent struct {
	id agentkit.AgentID
}

func (s stubAgent) ID() agentkit.AgentID { return s.id }

func (s stubAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }

func TestDispatchAgentUsePersistsBind(t *testing.T) {
	t.Parallel()

	mem, err := session.NewMemory(session.MemoryConfig{ID: "cli:test"})
	if err != nil {
		t.Fatal(err)
	}
	store := session.NewStaticStore(mem)
	reg, err := NewFromProviders(Config{}, []agentkit.CommandProvider{
		stubProvider{commands: []agentkit.Command{
			agent.Command([]agentkit.Agent{stubAgent{id: "reviewer"}}, store, "reviewer"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:test", Workspace: "cli:test"})
	out, err := reg.Dispatch(ctx, "agent", "use reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "reviewer") {
		t.Fatalf("output = %q", out)
	}
	if got, err := store.AgentBind(ctx, "cli:test"); err != nil || got != "reviewer" {
		t.Fatalf("bound agent = %q, err = %v", got, err)
	}
}
