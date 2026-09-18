package sessstore_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestStoreAgentBindFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	runtimeStore, ok := store.(agentkit.SessionRuntimeStore)
	if !ok {
		t.Fatal("expected SessionRuntimeStore")
	}

	const sessionID = agentkit.SessionID("cli:test-bind")
	ctx := context.Background()
	if got, err := runtimeStore.AgentBind(ctx, sessionID); err != nil || got != "" {
		t.Fatalf("initial bind = %q, err = %v", got, err)
	}
	if err := runtimeStore.SetAgentBind(ctx, sessionID, "reviewer"); err != nil {
		t.Fatal(err)
	}
	if got, err := runtimeStore.AgentBind(ctx, sessionID); err != nil || got != "reviewer" {
		t.Fatalf("bind = %q, err = %v", got, err)
	}

	path := filepath.Join(dir, "cli_test-bind", "runtime.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "reviewer") {
		t.Fatalf("file = %s", data)
	}

	reopened, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	reopenedRuntime, ok := reopened.(agentkit.SessionRuntimeStore)
	if !ok {
		t.Fatal("expected SessionRuntimeStore")
	}
	if got, err := reopenedRuntime.AgentBind(ctx, sessionID); err != nil || got != "reviewer" {
		t.Fatalf("reopened bind = %q, err = %v", got, err)
	}
}

func TestStaticStoreAgentBind(t *testing.T) {
	t.Parallel()

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "mem-bind"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
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
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
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

	reopened, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "."}, sessstore.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.(agentkit.ActiveSessionStore).ActiveSession(context.Background(), key); err != nil || got != "slack:C001:t:123:u:U111:new:20260829" {
		t.Fatalf("reopened active = %q, err = %v", got, err)
	}
}
