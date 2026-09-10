package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestPrepareToolResultForStorageSpillsLargeOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	ctx := session.WithWorkspaceService(context.Background(), ws)

	full := strings.Repeat("line\n", 5000)
	stored, err := session.PrepareToolResultForStorage(ctx, agentkit.SessionID("sess-a"), agentkit.ToolResult{
		ID:      "call-1",
		Name:    "bash",
		Content: full,
	}, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Content) >= len(full) {
		t.Fatalf("expected truncated view, content len=%d", len(stored.Content))
	}
	spill := stored.Audit[session.AuditSpillPath]
	if spill == "" {
		t.Fatal("missing spill_path audit")
	}
	if !strings.Contains(stored.Content, "Full output: "+spill) {
		t.Fatalf("content = %q", stored.Content)
	}
	if len(stored.Content) > 200 {
		t.Fatalf("view with hint should fit maxViewBytes=200, len=%d", len(stored.Content))
	}
	abs, err := session.SpillPathAbs(ctx, ws, spill)
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != full {
		t.Fatalf("spill bytes = %d want %d", len(onDisk), len(full))
	}
}

func TestPrepareToolResultForStorageWithoutWorkspaceTruncates(t *testing.T) {
	t.Parallel()

	full := strings.Repeat("x", 9000)
	stored, err := session.PrepareToolResultForStorage(context.Background(), agentkit.SessionID("s"), agentkit.ToolResult{
		ID:      "c",
		Name:    "bash",
		Content: full,
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Audit != nil && stored.Audit[session.AuditSpillPath] != "" {
		t.Fatal("unexpected spill without workspace")
	}
	if !strings.HasSuffix(stored.Content, "\n...[truncated]") {
		t.Fatalf("content not truncated: len=%d", len(stored.Content))
	}
}

func TestPrepareToolResultForStorageSpillWriteFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	ctx := session.WithWorkspaceService(context.Background(), ws)

	rel := "work/tool-spill/sess-b/call-2.txt"
	abs, err := ws.Resolve(ctx, rel)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(abs, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err = session.PrepareToolResultForStorage(ctx, agentkit.SessionID("sess-b"), agentkit.ToolResult{
		ID:      "call-2",
		Name:    "bash",
		Content: strings.Repeat("x", 500),
	}, 100)
	if err == nil {
		t.Fatal("expected spill write error")
	}
}
