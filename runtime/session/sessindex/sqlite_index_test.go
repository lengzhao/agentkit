package sessindex_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessindex"
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
	idx, err := sessindex.NewSQLiteIndex(sessindex.SQLiteIndexConfig{}, sessindex.SQLiteIndexDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := idx.SyncSessions(ctx); err != nil {
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

// Regression: list must order by last activity (file mtime), not by the
// largest per-session seq — a quiet recent session outranks a busy stale one.
func TestSQLiteIndexListSessionsOrdersByRecency(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string, seqs int, mtime time.Time) {
		var lines []byte
		for seq := 1; seq <= seqs; seq++ {
			lines = append(lines, []byte(`{"id":"","seq":`+strconv.Itoa(seq)+`,"sessionID":"`+name+`","agentID":"a","type":"user/message","data":{"role":"user","content":[{"type":"text","text":"`+text+`"}]},"createdAt":"`+mtime.UTC().Format(time.RFC3339)+`"}`+"\n")...)
		}
		path := filepath.Join(sessionsDir, name+".jsonl")
		if err := os.WriteFile(path, lines, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	write("cli_busy_stale", "old discussion", 30, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	write("cli_quiet_recent", "fresh question", 1, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))

	ws := rtworkspace.Static(dir)
	idx, err := sessindex.NewSQLiteIndex(sessindex.SQLiteIndexConfig{}, sessindex.SQLiteIndexDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := idx.SyncSessions(ctx); err != nil {
		t.Fatal(err)
	}
	sessions, err := idx.ListSessions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %+v", sessions)
	}
	if sessions[0].SessionID != "cli_quiet_recent" {
		t.Fatalf("first session = %q, want cli_quiet_recent (recency order)", sessions[0].SessionID)
	}
}

func TestWorkspaceKeyFromLocalDir(t *testing.T) {
	t.Parallel()
	if got := rctx.WorkspaceKeyFromLocalDir("slack_C001", false); got != "slack:C001" {
		t.Fatalf("got %q", got)
	}
}
