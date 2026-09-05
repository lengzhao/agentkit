package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestStoreCommands(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(dir)})
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

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:default", Workspace: "cli:default"})
	for _, cmd := range commands {
		switch cmd.Name() {
		case "new":
			out, err := cmd.CommandExec(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(out, "cli:") {
				t.Fatalf("unexpected new session id: %q", out)
			}
		case "session":
			if _, err := cmd.CommandExec(ctx, ""); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected command %q", cmd.Name())
		}
	}
}

func TestNewCommandForNonCLIOnlyUpdatesActiveSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider := store.(agentkit.CommandProvider)
	var newCmd agentkit.Command
	for _, cmd := range provider.Commands() {
		if cmd.Name() == "new" {
			newCmd = cmd
			break
		}
	}
	if newCmd == nil {
		t.Fatal("missing /new command")
	}

	stable := agentkit.SessionID("slack:C001:t:123:u:U111")
	entry := session.ApplyScope(stable, session.ScopeChannel, "U111")
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(entry),
		Workspace:    string(entry),
		Route:        agentkit.SessionRoute("slack", string(stable)),
	})
	out, err := newCmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, string(entry)+":new:") {
		t.Fatalf("new logical session = %q", out)
	}
	active, err := store.(agentkit.ActiveSessionStore).ActiveSession(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	if active != agentkit.SessionID(out) {
		t.Fatalf("active session = %q, want %q", active, out)
	}
	if _, err := os.Lstat(filepath.Join(dir, session.CLICurrentLinkName)); !os.IsNotExist(err) {
		t.Fatalf("cli current link err = %v, want not exist", err)
	}
	current, err := store.(session.CLICurrentStore).ResolveCLICurrent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current != session.DefaultCLISessionID {
		t.Fatalf("cli current = %q, want default", current)
	}
}
