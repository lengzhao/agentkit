package session_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestSanitizeModelMessageForStorageStripsImageData(t *testing.T) {
	t.Parallel()

	msg := session.SanitizeModelMessageForStorage(agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{
			{Type: "text", Text: "extract this"},
			{Type: "image_url", URL: "data:image/png;base64," + strings.Repeat("A", 1024), MIME: "image/png"},
		},
	}, 0)

	if len(msg.Content) != 1 || msg.Content[0].Text != "extract this" {
		t.Fatalf("content = %#v", msg.Content)
	}
}

func TestSanitizeModelMessageForStorageKeepsAttachmentRef(t *testing.T) {
	t.Parallel()

	msg := session.SanitizeModelMessageForStorage(agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{{
			Type:   "image_url",
			URL:    "data:image/png;base64,abc",
			Source: "upload/shot.png",
			MIME:   "image/png",
		}},
	}, 0)
	if len(msg.Content) != 1 {
		t.Fatalf("content = %#v", msg.Content)
	}
	if msg.Content[0].Type != rtmedia.ContentTypeAttachmentRef || msg.Content[0].Source != "upload/shot.png" {
		t.Fatalf("content = %#v", msg.Content[0])
	}
}

func TestSanitizeModelMessageForStorageTruncatesUserText(t *testing.T) {
	t.Parallel()

	msg := session.SanitizeModelMessageForStorage(agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("x", 9000)}},
	}, 100)
	if !strings.HasSuffix(msg.Content[0].Text, "\n...[truncated]") {
		t.Fatalf("text not truncated: len=%d", len(msg.Content[0].Text))
	}
}

func TestSanitizeModelMessageForStorageKeepsFullChatTextByDefault(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("y", 12000)
	for _, role := range []string{"user", "assistant"} {
		msg := session.SanitizeModelMessageForStorage(agentkit.ModelMessage{
			Role:    role,
			Content: []agentkit.ContentPart{{Type: "text", Text: long}},
		}, 0)
		if msg.Content[0].Text != long {
			t.Fatalf("%s text truncated: len=%d want %d", role, len(msg.Content[0].Text), len(long))
		}
	}
}

func TestSanitizeModelMessageForStorageKeepsFullToolCallInput(t *testing.T) {
	t.Parallel()

	largeContent := strings.Repeat("a", 9000)
	rawInput := []byte(`{"path":"skills/chatai-cs/SKILL.md","content":"` + largeContent + `"}`)
	msg := session.SanitizeModelMessageForStorage(agentkit.ModelMessage{
		Role: "assistant",
		ToolCalls: []agentkit.ToolCall{{
			ID:    "write-1",
			Name:  "write",
			Input: rawInput,
		}},
	}, 100)

	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(msg.ToolCalls))
	}
	if string(msg.ToolCalls[0].Input) != string(rawInput) {
		t.Fatalf("tool call input truncated: got %d bytes want %d", len(msg.ToolCalls[0].Input), len(rawInput))
	}
	full := agentkit.ModelMessage{Role: "assistant", ToolCalls: msg.ToolCalls}
	if _, err := json.Marshal(full); err != nil {
		t.Fatalf("marshal sanitized assistant message: %v", err)
	}
}

func TestSanitizeToolCallInvalidJSONPlaceholder(t *testing.T) {
	t.Parallel()

	call := session.SanitizeToolCall(agentkit.ToolCall{
		ID:    "x",
		Name:  "write",
		Input: json.RawMessage(`{"path":"x","content":"`),
	})
	if !json.Valid(call.Input) {
		t.Fatalf("invalid JSON: %s", string(call.Input))
	}
	if !strings.Contains(string(call.Input), "_storage_note") {
		t.Fatalf("expected placeholder, got %s", string(call.Input))
	}
}

func TestAppendMessageStoresSanitized(t *testing.T) {
	t.Parallel()

	mem, err := session.NewMemory(session.MemoryConfig{ID: "mem-sanitize"})
	if err != nil {
		t.Fatal(err)
	}
	raw := agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{
			{Type: "text", Text: "hello"},
			{Type: "image_url", URL: "data:image/png;base64,abc", Source: "upload/a.png"},
		},
	}
	if err := session.AppendMessage(context.Background(), mem, "assistant", agentkit.EventUserMessage, raw); err != nil {
		t.Fatal(err)
	}
	events, err := mem.Read(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(events[0].Data), "data:image") {
		t.Fatal("raw image data persisted to session")
	}
	if !strings.Contains(string(events[0].Data), `"type":"attachment_ref"`) {
		t.Fatalf("data = %s", string(events[0].Data))
	}
}
