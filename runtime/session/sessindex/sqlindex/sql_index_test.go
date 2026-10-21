package sqlindex_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessindex "github.com/lengzhao/agentkit/runtime/session/sessindex/sqlindex"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
)

func TestSQLIndexSyncSearchListScroll(t *testing.T) {
	t.Parallel()

	const dsn = "file:sqlindex?mode=memory&cache=shared"
	ctx := context.Background()

	store, err := sessstore.NewSQLStore(sessstore.SQLStoreConfig{Driver: "sqlite", DSN: dsn}, sessstore.SQLStoreDeps{})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(ctx, agentkit.SessionID("cli:sql-index"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendMessage(ctx, sess, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "deploy kubernetes last week"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendMessage(ctx, sess, "agent", agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "kubernetes deploy done"}},
	}); err != nil {
		t.Fatal(err)
	}

	idx, err := sessindex.NewSQLIndex(sessindex.SQLIndexConfig{Driver: "sqlite", DSN: dsn}, sessindex.SQLIndexDeps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.SyncSessions(ctx); err != nil {
		t.Fatal(err)
	}

	hits, err := idx.Search(ctx, "kubernetes", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}

	sessions, err := idx.ListSessions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].SessionID != "cli:sql-index" || sessions[0].MessageCount != 2 {
		t.Fatalf("sessions = %+v", sessions)
	}

	rows, err := idx.ScrollMessages(ctx, "cli:sql-index", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Role != "user" || rows[1].Role != "assistant" {
		t.Fatalf("rows = %+v", rows)
	}

	// Incremental sync: append one more message, sync again, expect no duplicates.
	if err := sessevents.Default.AppendMessage(ctx, sess, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "and scale it"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := idx.SyncSessions(ctx); err != nil {
		t.Fatal(err)
	}
	sessions, err = idx.ListSessions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].MessageCount != 3 {
		t.Fatalf("after incremental sync sessions = %+v", sessions)
	}
}

// Regression: list must order by last activity (event CreatedAt), not by the
// largest per-session seq — a quiet recent session outranks a busy stale one.
func TestSQLIndexListSessionsOrdersByRecency(t *testing.T) {
	t.Parallel()

	const dsn = "file:sqlindex-recency?mode=memory&cache=shared"
	ctx := context.Background()

	// Create the events schema, then seed two sessions directly.
	if _, err := sessstore.NewSQLStore(sessstore.SQLStoreConfig{Driver: "sqlite", DSN: dsn}, sessstore.SQLStoreDeps{}); err != nil {
		t.Fatal(err)
	}
	db, err := sessstore.OpenSQLDB("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	seed := func(sessionID string, seqs int, created time.Time) {
		for seq := 1; seq <= seqs; seq++ {
			payload := fmt.Sprintf(`{"id":"","seq":%d,"sessionID":%q,"agentID":"a","type":"user/message","data":{"role":"user","content":[{"type":"text","text":"note"}]},"createdAt":%q}`,
				seq, sessionID, created.UTC().Format(time.RFC3339))
			if _, err := db.Exec(`INSERT INTO ak_session_events (session_id, seq, payload) VALUES (?, ?, ?)`, sessionID, seq, payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	seed("cli:busy-stale", 30, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	seed("cli:quiet-recent", 1, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))

	idx, err := sessindex.NewSQLIndex(sessindex.SQLIndexConfig{Driver: "sqlite", DSN: dsn}, sessindex.SQLIndexDeps{})
	if err != nil {
		t.Fatal(err)
	}
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
	if sessions[0].SessionID != "cli:quiet-recent" {
		t.Fatalf("first session = %q, want cli:quiet-recent (recency order)", sessions[0].SessionID)
	}
	if sessions[0].LastMod.IsZero() {
		t.Fatal("LastMod should come from max_created_at_ns")
	}
}

// The shared SQL store spans channels; index queries must stay inside the
// caller's workspace (channel), never leaking other channels' sessions.
func TestSQLIndexTenantScoping(t *testing.T) {
	t.Parallel()

	const dsn = "file:sqlindex-tenant?mode=memory&cache=shared"
	ctx := context.Background()

	if _, err := sessstore.NewSQLStore(sessstore.SQLStoreConfig{Driver: "sqlite", DSN: dsn}, sessstore.SQLStoreDeps{}); err != nil {
		t.Fatal(err)
	}
	db, err := sessstore.OpenSQLDB("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	seed := func(sessionID, text string) {
		payload := fmt.Sprintf(`{"id":"","seq":1,"sessionID":%q,"agentID":"a","type":"user/message","data":{"role":"user","content":[{"type":"text","text":%q}]},"createdAt":"2026-10-01T00:00:00Z"}`,
			sessionID, text)
		if _, err := db.Exec(`INSERT INTO ak_session_events (session_id, seq, payload) VALUES (?, ?, ?)`, sessionID, 1, payload); err != nil {
			t.Fatal(err)
		}
	}
	seed("lark:oc_a", "channel level note")
	seed("lark:oc_a:t:om_1", "thread note in same channel")
	seed("lark:oc_b:t:om_2", "other channel secret")

	idx, err := sessindex.NewSQLIndex(sessindex.SQLIndexConfig{Driver: "sqlite", DSN: dsn}, sessindex.SQLIndexDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ctxA := rctx.WithWorkspace(ctx, "lark:oc_a")
	if err := idx.SyncSessions(ctxA); err != nil {
		t.Fatal(err)
	}

	sessions, err := idx.ListSessions(ctxA, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("tenant list = %+v", sessions)
	}
	for _, s := range sessions {
		if s.SessionID == "lark:oc_b:t:om_2" {
			t.Fatalf("cross-tenant session leaked: %+v", sessions)
		}
	}

	hits, err := idx.Search(ctxA, "secret", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("cross-tenant search hit leaked: %+v", hits)
	}

	if _, err := idx.ScrollMessages(ctxA, "lark:oc_b:t:om_2", 0, 0, 0); err == nil {
		t.Fatal("scroll on foreign session should fail")
	}

	// scope=global opts out of channel scoping explicitly.
	ctxGlobal := rctx.WithSessionIndexGlobal(ctxA)
	if err := idx.SyncSessions(ctxGlobal); err != nil {
		t.Fatal(err)
	}
	sessions, err = idx.ListSessions(ctxGlobal, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Fatalf("global list = %+v", sessions)
	}
	hits, err = idx.Search(ctxGlobal, "secret", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("global search = %+v", hits)
	}
	if _, err := idx.ScrollMessages(ctxGlobal, "lark:oc_b:t:om_2", 0, 0, 0); err != nil {
		t.Fatalf("global scroll: %v", err)
	}
}
