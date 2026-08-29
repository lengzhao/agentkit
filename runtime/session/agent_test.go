package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestStoreAgentBindFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	bindStore, ok := store.(agentkit.AgentBindStore)
	if !ok {
		t.Fatal("expected AgentBindStore")
	}

	const sessionID = agentkit.SessionID("cli:test-bind")
	ctx := context.Background()
	if got, err := bindStore.AgentBind(ctx, sessionID); err != nil || got != "" {
		t.Fatalf("initial bind = %q, err = %v", got, err)
	}
	if err := bindStore.SetAgentBind(ctx, sessionID, "reviewer"); err != nil {
		t.Fatal(err)
	}
	if got, err := bindStore.AgentBind(ctx, sessionID); err != nil || got != "reviewer" {
		t.Fatalf("bind = %q, err = %v", got, err)
	}

	path := filepath.Join(dir, "cli_test-bind", "agent.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "reviewer") {
		t.Fatalf("file = %s", data)
	}

	reopened, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	rebind, ok := reopened.(agentkit.AgentBindStore)
	if !ok {
		t.Fatal("expected AgentBindStore")
	}
	if got, err := rebind.AgentBind(ctx, sessionID); err != nil || got != "reviewer" {
		t.Fatalf("reopened bind = %q, err = %v", got, err)
	}
}

func TestStaticStoreAgentBind(t *testing.T) {
	t.Parallel()

	mem, err := session.NewMemory(session.MemoryConfig{ID: "mem-bind"})
	if err != nil {
		t.Fatal(err)
	}
	store := session.NewStaticStore(mem)
	ctx := context.Background()
	if err := store.SetAgentBind(ctx, "mem-bind", "coder"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.AgentBind(ctx, "mem-bind"); err != nil || got != "coder" {
		t.Fatalf("bind = %q, err = %v", got, err)
	}
}

func TestStoreActiveSessionFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	active, ok := store.(agentkit.ActiveSessionStore)
	if !ok {
		t.Fatal("expected ActiveSessionStore")
	}

	const key = agentkit.SessionID("slack:C001:t:123:u:U111")
	if got, err := active.ActiveSession(context.Background(), key); err != nil || got != key {
		t.Fatalf("initial active = %q, err = %v", got, err)
	}
	if err := active.SetActiveSession(context.Background(), key, "slack:C001:t:123:u:U111:new:20260829"); err != nil {
		t.Fatal(err)
	}
	if got, err := active.ActiveSession(context.Background(), key); err != nil || got != "slack:C001:t:123:u:U111:new:20260829" {
		t.Fatalf("active = %q, err = %v", got, err)
	}

	path := filepath.Join(dir, "slack_C001_t_123_u_U111", "current.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("active session file: %v", err)
	}

	reopened, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.(agentkit.ActiveSessionStore).ActiveSession(context.Background(), key); err != nil || got != "slack:C001:t:123:u:U111:new:20260829" {
		t.Fatalf("reopened active = %q, err = %v", got, err)
	}
}
