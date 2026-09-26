package sessindex_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session/sessindex"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
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
