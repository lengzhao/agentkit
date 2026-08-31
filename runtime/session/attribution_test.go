package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func userCtx(userID string) context.Context {
	return context.WithValue(context.Background(), agentkit.KeyUserID, userID)
}

func textMessage(role, text string) agentkit.ModelMessage {
	return agentkit.ModelMessage{
		Role:    role,
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}
}

func firstText(msg agentkit.ModelMessage) string {
	for _, part := range msg.Content {
		if part.Type == "text" {
			return part.Text
		}
	}
	return ""
}

func attributionTestStore(t *testing.T, dir string) agentkit.SessionStore {
	t.Helper()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestSharedSessionReplaysStoredInjectPrefix(t *testing.T) {
	t.Parallel()

	store := attributionTestStore(t, t.TempDir())
	id := session.SlackSessionIDForScope(session.ScopeChannel, "C001", "", "U111")
	if other := session.SlackSessionIDForScope(session.ScopeChannel, "C001", "", "U222"); other != id {
		t.Fatalf("channel scope split the session: %q vs %q", id, other)
	}

	sess, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	if err := session.AppendMessage(userCtx("U111"), sess, "coder", agentkit.EventUserMessage, textMessage("user", "[agentkit sender_id=U111]\n改一下 README")); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(context.Background(), sess, "coder", agentkit.EventAssistantMessage, textMessage("assistant", "好的")); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(userCtx("U222"), sess, "coder", agentkit.EventUserMessage, textMessage("user", "[agentkit sender_id=U222]\n顺便跑一下测试")); err != nil {
		t.Fatal(err)
	}

	msgs, err := sess.DeriveMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("derived %d messages, want 3", len(msgs))
	}
	if got := firstText(msgs[0]); !strings.Contains(got, "sender_id=U111") || !strings.Contains(got, "改一下 README") {
		t.Fatalf("first user message = %q", got)
	}
	if got := firstText(msgs[2]); !strings.Contains(got, "sender_id=U222") {
		t.Fatalf("second user message = %q", got)
	}
	if got := firstText(msgs[1]); strings.Contains(got, "[agentkit ") {
		t.Fatalf("assistant message was attributed: %q", got)
	}
}

// Single-user transports set no UserID, and their history must derive exactly as stored.
func TestNoUserIDLeavesHistoryUntouched(t *testing.T) {
	t.Parallel()

	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(context.Background(), session.DefaultCLISessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(context.Background(), sess, "coder", agentkit.EventUserMessage, textMessage("user", "列出目录")); err != nil {
		t.Fatal(err)
	}
	msgs, err := sess.DeriveMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("derived %d messages, want 1", len(msgs))
	}
	if got := firstText(msgs[0]); got != "列出目录" {
		t.Fatalf("message = %q, want unwrapped", got)
	}
}

func TestStoredInjectPrefixSurvivesReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	id := agentkit.SessionID("slack:C001")

	store := attributionTestStore(t, dir)
	sess, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(userCtx("U111"), sess, "coder", agentkit.EventUserMessage, textMessage("user", "[agentkit sender_id=U111]\nhi")); err != nil {
		t.Fatal(err)
	}

	reopened := attributionTestStore(t, dir)
	sess2, err := reopened.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := sess2.DeriveMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("derived %d messages, want 1", len(msgs))
	}
	if got := firstText(msgs[0]); !strings.Contains(got, "sender_id=U111") {
		t.Fatalf("reloaded message = %q", got)
	}
}

func TestImageOnlyMessageDerivesUnchanged(t *testing.T) {
	t.Parallel()

	store := attributionTestStore(t, t.TempDir())
	sess, err := store.Get(context.Background(), agentkit.SessionID("slack:C001"))
	if err != nil {
		t.Fatal(err)
	}
	msg := agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "image", URL: "https://example.com/a.png"}},
	}
	if err := session.AppendMessage(userCtx("U111"), sess, "coder", agentkit.EventUserMessage, msg); err != nil {
		t.Fatal(err)
	}
	msgs, err := sess.DeriveMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("derived %d messages, want 1", len(msgs))
	}
	if n := len(msgs[0].Content); n != 1 {
		t.Fatalf("content parts = %d, want 1", n)
	}
	if msgs[0].Content[0].Type != "image" {
		t.Fatalf("image part changed: %+v", msgs[0].Content)
	}
}
