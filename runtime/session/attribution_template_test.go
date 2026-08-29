package session_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func userCtxWithMetadata(userID string, metadata map[string]any) context.Context {
	ctx := userCtx(userID)
	if len(metadata) > 0 {
		ctx = context.WithValue(ctx, agentkit.KeyMessageMetadata, metadata)
	}
	return ctx
}

func TestDefaultTemplateWrapsUserID(t *testing.T) {
	t.Parallel()

	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(context.Background(), agentkit.SessionID("slack:C001"))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(userCtx("demo"), sess, "coder", agentkit.EventUserMessage, textMessage("user", "今天几号")); err != nil {
		t.Fatal(err)
	}
	msgs, err := sess.DeriveMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := firstText(msgs[0])
	want := "<user id=\"demo\">\n今天几号\n</user>"
	if got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestPassthroughTemplateLeavesTextUntouched(t *testing.T) {
	t.Parallel()

	store, err := session.NewStore(session.StoreConfig{
		Dir:                 ".",
		UserMessageTemplate: "{{.Text}}",
	}, session.StoreDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(context.Background(), agentkit.SessionID("slack:C001"))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(userCtx("demo"), sess, "coder", agentkit.EventUserMessage, textMessage("user", "今天几号")); err != nil {
		t.Fatal(err)
	}
	msgs, err := sess.DeriveMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := firstText(msgs[0]); got != "今天几号" {
		t.Fatalf("message = %q, want raw stored text", got)
	}
}

func TestCustomUserMessageTemplateWithMetadata(t *testing.T) {
	t.Parallel()

	store, err := session.NewStore(session.StoreConfig{
		Dir:                 ".",
		UserMessageTemplate: "[user id={{.UserID}} name={{index .Metadata \"name\"}}]\n{{.Text}}",
	}, session.StoreDeps{Workspace: workspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(context.Background(), agentkit.SessionID("slack:C001"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := userCtxWithMetadata("demo", map[string]any{"name": "Alice"})
	if err := session.AppendMessage(ctx, sess, "coder", agentkit.EventUserMessage, textMessage("user", "今天几号")); err != nil {
		t.Fatal(err)
	}
	msgs, err := sess.DeriveMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := firstText(msgs[0])
	want := "[user id=demo name=Alice]\n今天几号"
	if got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}
