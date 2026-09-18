package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/runtime/rctx"
	rw "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestPromptBodyGlobalAndLocal(t *testing.T) {
	t.Parallel()

	global := t.TempDir()
	localBase := t.TempDir()
	tenant := "chat-api:default_channel6"
	if err := os.MkdirAll(filepath.Join(localBase, "chat-api_default_channel6"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, "memory.md"), []byte("# memory.md\n\nglobal fact\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localBase, "chat-api_default_channel6", "memory.md"), []byte("# memory.md\n\nlocal fact\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := rw.NewTenant(rw.TenantConfig{Global: global, LocalBase: localBase, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	mem, err := New(Config{}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := rctx.WithWorkspace(context.Background(), tenant)
	body, err := mem.PromptBody(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "global fact") || !strings.Contains(body, "local fact") {
		t.Fatalf("body = %q", body)
	}
}

func TestPromptBodyMemoryRootSubdir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws, err := rw.New(rw.Config{Local: root, Scope: "local"})
	if err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "tenant-memory")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "memory.md"), []byte("# memory.md\n\nfrom subdir root\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "memory.md"), []byte("# memory.md\n\nstale local root\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mem, err := New(Config{MemoryRoot: "tenant-memory"}, Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	body, err := mem.PromptBody(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "from subdir root") {
		t.Fatalf("body = %q", body)
	}
	if strings.Contains(body, "stale local root") {
		t.Fatalf("should not read tenant root memory.md when memoryRoot is subdir: %q", body)
	}
}
