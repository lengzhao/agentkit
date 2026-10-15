package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
)

type stubBindAgent struct {
	id agentkit.AgentID
}

func (s stubBindAgent) ID() agentkit.AgentID { return s.id }

func (s stubBindAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }

func TestAgentUseCommandPersistsBind(t *testing.T) {
	t.Parallel()

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "cli:test"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	cmd := AgentCommand([]agentkit.Agent{stubBindAgent{id: "reviewer"}}, store, "reviewer", nil)

	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:test", Workspace: "cli:test"})
	out, err := cmd.CommandExec(ctx, "use reviewer")
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
