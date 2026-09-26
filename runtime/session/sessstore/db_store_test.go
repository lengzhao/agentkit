package sessstore_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
)

func TestSQLStoreIsolatesSessionsByID(t *testing.T) {
	t.Parallel()

	store, err := sessstore.NewSQLStore(sessstore.SQLStoreConfig{
		Driver: "sqlite",
		DSN:    "file:sqlstore_isolate?mode=memory&cache=shared",
	}, sessstore.SQLStoreDeps{})
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

	if err := sessevents.Default.AppendMessage(ctx, s1, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "hello from C001"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendMessage(ctx, s2, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
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
}

func TestSQLStorePersistsAcrossReopen(t *testing.T) {
	t.Parallel()

	const dsn = "file:sqlstore_persist?mode=memory&cache=shared"
	open := func() agentkit.SessionStore {
		store, err := sessstore.NewSQLStore(sessstore.SQLStoreConfig{
			Driver: "sqlite",
			DSN:    dsn,
		}, sessstore.SQLStoreDeps{})
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		return store
	}

	ctx := context.Background()
	sessionID := agentkit.SessionID("cli:sql-persist")

	store1 := open()
	s1, err := store1.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendMessage(ctx, s1, "agent", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "round one"}},
	}); err != nil {
		t.Fatal(err)
	}

	store2 := open()
	s2, err := store2.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := s2.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Content[0].Text != "round one" {
		t.Fatalf("after reopen: %+v", msgs)
	}
}

func TestSQLStoreActiveSession(t *testing.T) {
	t.Parallel()

	store, err := sessstore.NewSQLStore(sessstore.SQLStoreConfig{
		Driver: "sqlite",
		DSN:    "file:sqlstore_active?mode=memory&cache=shared",
	}, sessstore.SQLStoreDeps{})
	if err != nil {
		t.Fatal(err)
	}
	active, ok := store.(agentkit.ActiveSessionStore)
	if !ok {
		t.Fatal("expected ActiveSessionStore")
	}
	ctx := context.Background()
	key := agentkit.SessionID("cli:default")
	logical := agentkit.SessionID("cli:default:new:20260101")
	if err := active.SetActiveSession(ctx, key, logical); err != nil {
		t.Fatal(err)
	}
	got, err := active.ActiveSession(ctx, key)
	if err != nil || got != logical {
		t.Fatalf("active = %q err=%v", got, err)
	}
}
