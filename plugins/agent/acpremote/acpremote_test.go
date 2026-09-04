package acpremote

import (
	"context"
	"fmt"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestModelMessageToPrompt(t *testing.T) {
	blocks := modelMessageToPrompt(agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{
			{Type: "text", Text: "hello"},
		},
	})
	if len(blocks) != 1 {
		t.Fatalf("blocks: got %d want 1", len(blocks))
	}
	if blocks[0].Text == nil || blocks[0].Text.Text != "hello" {
		t.Fatalf("unexpected block: %+v", blocks[0])
	}
}

func TestReadWriteTextFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sub/file.txt"
	if err := writeTextFile(path, "line1\nline2\nline3"); err != nil {
		t.Fatal(err)
	}
	got, err := readTextFile(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "line1\nline2\nline3" {
		t.Fatalf("got %q", got)
	}
	line := 2
	limit := 1
	got, err = readTextFile(path, &line, &limit)
	if err != nil {
		t.Fatal(err)
	}
	if got != "line2" {
		t.Fatalf("slice got %q", got)
	}
}

func TestUpdateEmitterStreamsText(t *testing.T) {
	var events []agentkit.OutboundEvent
	emit := func(_ context.Context, ev agentkit.OutboundEvent) error {
		events = append(events, ev)
		return nil
	}
	e := newUpdateEmitter(t.Context(), "sess-1", "acp", emit)
	if err := e.consume(acp.SessionNotification{
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.TextBlock("hi "),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.consume(acp.SessionNotification{
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.TextBlock("there"),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.finalize(); err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("events: %d", len(events))
	}
	if events[0].Type != agentkit.EventMessageStart {
		t.Fatalf("first event: %s", events[0].Type)
	}
	if events[len(events)-1].Type != agentkit.EventMessageEnd {
		t.Fatalf("last event: %s", events[len(events)-1].Type)
	}
	msg := e.assistantMessage()
	if msg.Content[0].Text != "hi there" {
		t.Fatalf("assistant: %+v", msg)
	}
}

func TestAutoApprovePermission(t *testing.T) {
	resp := autoApprovePermission(acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "allow-once", Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow"},
		},
	})
	if resp.Outcome.Selected == nil || resp.Outcome.Selected.OptionId != "allow-once" {
		t.Fatalf("unexpected outcome: %+v", resp.Outcome)
	}
}

func TestIsCursorAuthError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("network timeout"), false},
		{fmt.Errorf("acp initialize: peer disconnected"), true},
		{fmt.Errorf("acp authenticate: [unauthenticated]"), true},
		{fmt.Errorf("not logged in"), true},
	}
	for _, tc := range cases {
		if got := isCursorAuthError(tc.err); got != tc.want {
			t.Fatalf("isCursorAuthError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestRunTurnUsesStoreSessionID(t *testing.T) {
	t.Parallel()

	storeID := agentkit.SessionID("chat-api:default_channel:t:conv_abc1234567890123456789")
	effective := agentkit.SessionID("chat-api:default_channel")
	mem, err := session.NewMemory(session.MemoryConfig{ID: storeID})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recordingStore{sess: mem}
	rt, err := New(Config{
		ID:      "cursor",
		Command: []string{"/nonexistent/agent-acp-test-binary"},
	}, Deps{
		Workspace:    &stubWorkspace{},
		SessionStore: rec,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ctx = agenttest.LoopTurnContext(effective, storeID, "cursor")
	emit := func(context.Context, agentkit.OutboundEvent) error { return nil }
	_ = rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}},
		},
		Emit: emit,
	})

	if len(rec.requested) == 0 {
		t.Fatal("expected sessionStore.Get with store session id")
	}
	if rec.requested[0] != storeID {
		t.Fatalf("sessionStore.Get id = %q, want %q", rec.requested[0], storeID)
	}
}

type recordingStore struct {
	requested []agentkit.SessionID
	sess      agentkit.Session
}

func (r *recordingStore) Get(_ context.Context, id agentkit.SessionID) (agentkit.Session, error) {
	r.requested = append(r.requested, id)
	return r.sess, nil
}

func TestNewRequiresCommand(t *testing.T) {
	_, err := New(Config{}, Deps{Workspace: &stubWorkspace{}})
	if err == nil {
		t.Fatal("expected error")
	}
}

type stubWorkspace struct{}

func (stubWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	return rel, nil
}
