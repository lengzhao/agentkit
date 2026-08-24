package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestStoreCommands(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := store.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("expected session store to implement CommandProvider")
	}
	commands := provider.Commands()
	if len(commands) != 2 {
		t.Fatalf("commands=%d want 2", len(commands))
	}

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("cli:default"))
	for _, cmd := range commands {
		switch cmd.Name() {
		case "new":
			out, err := cmd.CommandExec(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out, "cli:") {
				t.Fatalf("unexpected new session id: %q", out)
			}
		case "session":
			if _, err := cmd.CommandExec(ctx); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected command %q", cmd.Name())
		}
	}
}
