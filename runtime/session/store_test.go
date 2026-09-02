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

func TestStoreIsolatesSessionsByID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	s1, err := store.Get(ctx, agentkit.SessionID("slack:C001"))
	if err != nil {
		t.Fatal(err)
	}
	s2, err := store.Get(ctx, agentkit.SessionID("slack:C002"))
	if err != nil {
		t.Fatal(err)
	}
	if s1.ID() == s2.ID() {
		t.Fatal("expected distinct session ids")
	}

	if err := session.AppendMessage(ctx, s1, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "hello from C001"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, s2, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "hello from C002"}},
	}); err != nil {
		t.Fatal(err)
	}

	msgs1, err := s1.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	msgs2, err := s2.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs1) != 1 || msgs1[0].Content[0].Text != "hello from C001" {
		t.Fatalf("session 1 messages: %+v", msgs1)
	}
	if len(msgs2) != 1 || msgs2[0].Content[0].Text != "hello from C002" {
		t.Fatalf("session 2 messages: %+v", msgs2)
	}

	path1 := filepath.Join(dir, "slack_C001.jsonl")
	path2 := filepath.Join(dir, "slack_C002.jsonl")
	if _, err := os.Stat(path1); err != nil {
		t.Fatalf("expected file %s: %v", path1, err)
	}
	if _, err := os.Stat(path2); err != nil {
		t.Fatalf("expected file %s: %v", path2, err)
	}
}

func TestStoreRejectsUnsafeSessionID(t *testing.T) {
	t.Parallel()

	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Get(context.Background(), agentkit.SessionID("../escape"))
	if err == nil {
		t.Fatal("expected error for unsafe session id")
	}
}

// TestReopenedSessionContinuesSeqNumbering guards the invariant every seq
// comparison depends on: compaction's cutoff, run-state scans and Read(from) all
// break silently if a reopened session restarts numbering at 1.
func TestReopenedSessionContinuesSeqNumbering(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	sessionID := agentkit.SessionID("test:reopen")

	open := func() agentkit.Session {
		t.Helper()
		store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
		if err != nil {
			t.Fatal(err)
		}
		sess, err := store.Get(ctx, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		return sess
	}

	first := open()
	for i := 0; i < 3; i++ {
		if err := session.AppendStepStart(ctx, first, "a", i); err != nil {
			t.Fatal(err)
		}
	}

	// A fresh process reopens the same file and keeps appending.
	second := open()
	seq, err := second.Append(ctx, agentkit.SessionEvent{AgentID: "a", Type: agentkit.EventTurnEnd})
	if err != nil {
		t.Fatal(err)
	}
	if seq != 4 {
		t.Fatalf("first seq after reopen = %d, want 4", seq)
	}

	events, err := session.ReadAllEvents(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}
	for i, ev := range events {
		if ev.Seq != agentkit.EventSeq(i+1) {
			t.Fatalf("event[%d] seq = %d, want %d (seq must stay monotonic)", i, ev.Seq, i+1)
		}
	}
}

func TestStoreLRUCacheReloadsFromDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	store, err := session.NewStore(session.StoreConfig{
		Dir:               ".",
		MaxCachedSessions: 2,
	}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}

	id1 := agentkit.SessionID("cache:s1")
	id2 := agentkit.SessionID("cache:s2")
	id3 := agentkit.SessionID("cache:s3")

	s1a, err := store.Get(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, s1a, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "persisted"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, id2); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, id3); err != nil {
		t.Fatal(err)
	}

	s1b, err := store.Get(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if s1a == s1b {
		t.Fatal("expected cache miss to reload session from disk")
	}
	msgs, err := s1b.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content[0].Text != "persisted" {
		t.Fatalf("reloaded messages: %+v", msgs)
	}
}

func TestStoreHeldSessionSurvivesCacheEviction(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()
	store, err := session.NewStore(session.StoreConfig{
		Dir:               ".",
		MaxCachedSessions: 1,
	}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}

	held, err := store.Get(ctx, agentkit.SessionID("held:s1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, held, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "still writable"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, agentkit.SessionID("held:s2")); err != nil {
		t.Fatal(err)
	}

	msgs, err := held.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content[0].Text != "still writable" {
		t.Fatalf("held session messages: %+v", msgs)
	}
}

func TestStoreEnsuresToolWorkDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}

	workDir := filepath.Join(dir, session.TenantToolWorkDir)
	if _, err := os.Stat(workDir); !os.IsNotExist(err) {
		t.Fatalf("work dir should not exist before first session: %v", err)
	}

	ctx := context.Background()
	if _, err := store.Get(ctx, agentkit.SessionID("chat-api:conv-1")); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(workDir)
	if err != nil {
		t.Fatalf("work dir should be created with sessions: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("work path should be a directory")
	}
}
