package acpremote

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit"
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
