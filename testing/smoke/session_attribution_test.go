package smoke_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func userContext(userID string) context.Context {
	return context.WithValue(context.Background(), agentkit.KeyUserID, userID)
}

// E2E-513: multi-user channel history derives with speaker attribution from userMessageTemplate.
func TestSmokeUserMessageTemplateAttributesSpeakers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{
		Workspace: workspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}

	effective := session.ApplyScope(
		session.BuildDeliverySessionID("slack", "C001", "", "U111"),
		session.ScopeChannel,
		"U111",
	)
	sess, err := store.Get(context.Background(), effective)
	if err != nil {
		t.Fatal(err)
	}

	if err := session.AppendMessage(userContext("U111"), sess, "smoke", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "改一下 README"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(context.Background(), sess, "smoke", agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "好的"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(userContext("U222"), sess, "smoke", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "顺便跑一下测试"}},
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := sess.DeriveMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("derived %d messages, want 3", len(msgs))
	}
	first := agenttest.ContentText(msgs[0])
	if !strings.Contains(first, `<user id="U111">`) || !strings.Contains(first, "改一下 README") {
		t.Fatalf("first user message = %q", first)
	}
	third := agenttest.ContentText(msgs[2])
	if !strings.Contains(third, `<user id="U222">`) {
		t.Fatalf("second user message = %q", third)
	}
	assistant := agenttest.ContentText(msgs[1])
	if strings.Contains(assistant, "<user id=") {
		t.Fatalf("assistant message was attributed: %q", assistant)
	}
}

func TestSmokeCustomUserMessageTemplate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{
		Dir:                 ".",
		UserMessageTemplate: "[{{.UserID}}] {{.Text}}",
	}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.Get(context.Background(), agentkit.SessionID("slack:C001"))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(userContext("alice"), sess, "smoke", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "hello channel"}},
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := sess.DeriveMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := agenttest.ContentText(msgs[0])
	if got != "[alice] hello channel" {
		t.Fatalf("derived message = %q", got)
	}
}
