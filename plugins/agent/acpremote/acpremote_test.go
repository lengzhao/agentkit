package acpremote

import (
	"context"
	"fmt"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/session"
	rttelemetry "github.com/lengzhao/agentkit/runtime/telemetry"
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

func TestUpdateEmitterRecordsGenerationAndToolObservations(t *testing.T) {
	rec := &rttelemetry.RecordingExporter{}
	ctx := rttelemetry.WithExporter(t.Context(), rec)
	e := newUpdateEmitter(ctx, "sess-1", "acp", func(context.Context, agentkit.OutboundEvent) error {
		return nil
	})

	if err := e.consume(acp.SessionNotification{
		Update: acp.SessionUpdate{
			AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
				Content: acp.TextBlock("checking files"),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.consume(acp.SessionNotification{
		Update: acp.SessionUpdate{
			ToolCall: &acp.SessionUpdateToolCall{
				ToolCallId: "call-1",
				Title:      "Read file",
				RawInput:   map[string]any{"path": "README.md"},
				Status:     acp.ToolCallStatusInProgress,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.consume(acp.SessionNotification{
		Update: acp.SessionUpdate{
			ToolCallUpdate: &acp.SessionToolCallUpdate{
				ToolCallId: "call-1",
				Content:    []acp.ToolCallContent{acp.ToolContent(acp.TextBlock("reading"))},
				Status:     acp.Ptr(acp.ToolCallStatusInProgress),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.consume(acp.SessionNotification{
		Update: acp.SessionUpdate{
			ToolCallUpdate: &acp.SessionToolCallUpdate{
				ToolCallId: "call-1",
				RawOutput:  "file contents",
				Status:     acp.Ptr(acp.ToolCallStatusCompleted),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.finalize(); err != nil {
		t.Fatal(err)
	}

	_, observations, _ := rec.Snapshot()
	if len(observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(observations))
	}
	byName := make(map[string]rttelemetry.RecordedObservation, len(observations))
	for _, observation := range observations {
		byName[observation.Meta.Name] = observation
	}
	generation := byName["acp.generation"]
	tool := byName["tool.Read file"]
	if generation.ID == "" || tool.ID == "" {
		t.Fatalf("observations = %#v", observations)
	}
	if generation.End.Output == "" {
		t.Fatal("generation output is empty")
	}
	if tool.Meta.Input != `{"path":"README.md"}` {
		t.Fatalf("tool input = %q", tool.Meta.Input)
	}
	if tool.End.Output != `"file contents"` {
		t.Fatalf("tool output = %q", tool.End.Output)
	}
	if tool.ParentID != generation.ID {
		t.Fatalf("tool parent = %q, want %q", tool.ParentID, generation.ID)
	}
	if generation.Meta.Kind != captelemetry.KindGeneration {
		t.Fatalf("generation kind = %q", generation.Meta.Kind)
	}
	if tool.Meta.Kind != captelemetry.KindTool {
		t.Fatalf("tool kind = %q", tool.Meta.Kind)
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

func TestRunTurnUsesResolvedSessionID(t *testing.T) {
	t.Parallel()

	storeID := agentkit.SessionID("chat-api:default_channel:t:conv_abc1234567890123456789")
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
	ctx = agenttest.TurnContext(storeID, "cursor")
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
