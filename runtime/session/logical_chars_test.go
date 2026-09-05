package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestAppendMessageRecordsLogicalCharsWhenTruncated(t *testing.T) {
	t.Parallel()

	mem, err := session.NewMemory(session.MemoryConfig{ID: "mem-logical"})
	if err != nil {
		t.Fatal(err)
	}
	raw := agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: strings.Repeat("x", 12000)}},
	}
	if err := session.AppendMessage(context.Background(), mem, "assistant", agentkit.EventUserMessage, raw); err != nil {
		t.Fatal(err)
	}
	events, err := mem.Read(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	got := 0
	switch v := events[0].Metadata[session.MetadataLogicalChars].(type) {
	case int:
		got = v
	case float64:
		got = int(v)
	}
	if got < 12000 {
		t.Fatalf("logical_chars = %v, want >= 12000", events[0].Metadata[session.MetadataLogicalChars])
	}
}

func TestHydrateOnlyLastUserMessage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "work", "upload")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(workDir, "old.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "new.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}

	ws := rtworkspace.Static(root)
	msgs := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: rtmedia.ContentTypeAttachmentRef, Source: "upload/old.png"}}},
		{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "ok"}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: rtmedia.ContentTypeAttachmentRef, Source: "upload/new.png"}}},
	}
	out, err := session.HydrateLocalAttachments(context.Background(), msgs, ws, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Content[0].Type != rtmedia.ContentTypeAttachmentRef {
		t.Fatalf("old user message hydrated unexpectedly: %#v", out[0].Content)
	}
	if out[2].Content[0].Type != "image_url" {
		t.Fatalf("latest user message = %#v", out[2].Content)
	}
}
