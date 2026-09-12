package learning

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/runtime/session"
	rw "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestJoinUnderMemoryRootGlobal(t *testing.T) {
	t.Parallel()
	if got := MemoryRelPath("global:.", ""); got != "global:memory.md" {
		t.Fatalf("MemoryRelPath=%q", got)
	}
	if got := stagedRelPath("global:."); got != "global:memory/.staged" {
		t.Fatalf("stagedRelPath=%q", got)
	}
}

func TestCaptureMemoryWritesToGlobalMemoryRoot(t *testing.T) {
	global := t.TempDir()
	localBase := t.TempDir()
	tenantDir := filepath.Join(localBase, "chat-api_default_channel6")
	if err := os.MkdirAll(tenantDir, 0o755); err != nil {
		t.Fatal(err)
	}
	svc, err := rw.NewTenant(rw.TenantConfig{
		Global:    global,
		LocalBase: localBase,
		Scope:     "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	learning, err := New(Config{MemoryRoot: "global:."}, Deps{
		Workspace:    svc,
		SessionStore: stubSessionStore{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := session.WithWorkspace(context.Background(), "chat-api:default_channel6")
	if _, err := learning.CaptureMemoryAdd(ctx, "nickname 小飞", "learn-memory"); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(global, "memory.md")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected global memory.md at %s: %v", want, err)
	}
	tenantMem := filepath.Join(tenantDir, "memory.md")
	if _, err := os.Stat(tenantMem); err == nil {
		t.Fatalf("should not write tenant-local %s", tenantMem)
	}
}
