package session_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestSQLiteIndexSearch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionsDir, "cli_default.jsonl")
	line := `{"id":"","seq":1,"sessionID":"cli:default","agentID":"coder","type":"user/message","data":{"role":"user","content":[{"type":"text","text":"deploy kubernetes last week"}]},"createdAt":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := rtworkspace.Static(dir)
	idx, err := session.NewSQLiteIndex(session.SQLiteIndexConfig{}, session.SQLiteIndexDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := idx.SyncSessions(ctx, sessionsDir); err != nil {
		t.Fatal(err)
	}
	hits, err := idx.Search(ctx, "kubernetes", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	if hits[0].SessionID != "cli_default" {
		t.Fatalf("session id = %q", hits[0].SessionID)
	}
	sessions, err := idx.ListSessions(ctx, 5)
	if err != nil || len(sessions) == 0 {
		t.Fatalf("list sessions: %v len=%d", err, len(sessions))
	}
	rows, err := idx.ScrollMessages(ctx, "cli_default", hits[0].Seq, 0, 2)
	if err != nil || len(rows) == 0 {
		t.Fatalf("scroll: %v len=%d", err, len(rows))
	}
}

func TestWorkspaceKeyFromLocalDir(t *testing.T) {
	t.Parallel()
	if got := rctx.WorkspaceKeyFromLocalDir("slack_C001", false); got != "slack:C001" {
		t.Fatalf("got %q", got)
	}
}
