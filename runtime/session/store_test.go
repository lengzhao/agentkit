package session_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestStoreIsolatesSessionsByID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: dir})
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

	store, err := session.NewStore(session.StoreConfig{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Get(context.Background(), agentkit.SessionID("../escape"))
	if err == nil {
		t.Fatal("expected error for unsafe session id")
	}
}
