package session_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestCLICurrentSymlinkRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	current, ok := store.(session.CLICurrentStore)
	if !ok {
		t.Fatal("expected CLICurrentStore")
	}
	ctx := context.Background()

	id := session.NewCLISessionID()
	if err := current.SetCLICurrent(ctx, id); err != nil {
		t.Fatal(err)
	}
	got, err := current.ResolveCLICurrent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != id {
		t.Fatalf("resolve=%q want=%q", got, id)
	}

	if err := current.SetCLICurrent(ctx, session.DefaultCLISessionID); err != nil {
		t.Fatal(err)
	}
	got, err = current.ResolveCLICurrent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != session.DefaultCLISessionID {
		t.Fatalf("resolve default=%q", got)
	}
	target, err := os.Readlink(filepath.Join(dir, session.CLICurrentLinkName))
	if err != nil {
		t.Fatal(err)
	}
	if target != "cli_default.jsonl" {
		t.Fatalf("link target=%q", target)
	}
}

func TestResolveCLICurrentCreatesDefaultLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	current := store.(session.CLICurrentStore)
	ctx := context.Background()

	got, err := current.ResolveCLICurrent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != session.DefaultCLISessionID {
		t.Fatalf("got=%q", got)
	}
	if _, err := os.Readlink(filepath.Join(dir, session.CLICurrentLinkName)); err != nil {
		t.Fatalf("expected symlink: %v", err)
	}
}

func TestNewCommandUpdatesCLICurrent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
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
	out, err := newCmd.CommandExec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := agentkit.SessionID(out)
	current := store.(session.CLICurrentStore)
	got, err := current.ResolveCLICurrent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != id {
		t.Fatalf("current=%q want=%q", got, id)
	}
}
