package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/media"
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
	if msg.Content[0].Type != media.ContentTypeAttachmentRef || msg.Content[0].Source != "upload/shot.png" {
		t.Fatalf("content = %#v", msg.Content[0])
	}
}

func TestSanitizeModelMessageForStorageTruncatesText(t *testing.T) {
	t.Parallel()

	msg := session.SanitizeModelMessageForStorage(agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("x", 9000)}},
	}, 100)
	if !strings.HasSuffix(msg.Content[0].Text, "\n...[truncated]") {
		t.Fatalf("text not truncated: len=%d", len(msg.Content[0].Text))
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
