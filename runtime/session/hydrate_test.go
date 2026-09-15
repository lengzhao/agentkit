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

func TestHydrateLocalAttachmentsReloadsWorkspaceImage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "work", "upload")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(workDir, "shot.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}

	ws := rtworkspace.Static(root)
	ctx := context.Background()
	msgs := []agentkit.ModelMessage{{
		Role: "user",
		Content: []agentkit.ContentPart{{
			Type:   rtmedia.ContentTypeAttachmentRef,
			Source: "upload/shot.png",
			MIME:   "image/png",
		}},
	}}
	out, err := session.HydrateLocalAttachments(ctx, msgs, ws, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].Content) != 1 {
		t.Fatalf("content = %#v", out[0].Content)
	}
	if out[0].Content[0].Type != "image_url" || out[0].Content[0].URL == "" {
		t.Fatalf("image part = %#v", out[0].Content[0])
	}
}

func TestHydrateLocalAttachmentsExtensionlessJPEG(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "work", "upload")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	if err := os.WriteFile(filepath.Join(workDir, "file_999_0"), jpeg, 0o644); err != nil {
		t.Fatal(err)
	}

	ws := rtworkspace.Static(root)
	ctx := context.Background()
	readResult := rtmedia.FormatReadImageResult("upload/file_999_0", "image/jpeg", int64(len(jpeg)))
	msgs := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "look"}}},
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "call-1", Name: "read"}}},
		{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "call-1", Name: "read", Content: readResult}}},
	}
	out, err := session.HydrateLocalAttachments(ctx, msgs, ws, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 {
		t.Fatalf("messages = %d, want 4", len(out))
	}
	if out[3].Content[0].Type != "image_url" || out[3].Content[0].URL == "" {
		t.Fatalf("image part = %#v", out[3].Content[0])
	}
}

func TestHydrateLocalAttachmentsInjectsReadToolVision(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "work", "upload")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(workDir, "shot.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}

	ws := rtworkspace.Static(root)
	ctx := context.Background()
	readResult := rtmedia.FormatReadImageResult("upload/shot.png", "image/png", int64(len(png)))
	msgs := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "look"}}},
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "call-1", Name: "read"}}},
		{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "call-1", Name: "read", Content: readResult}}},
	}
	out, err := session.HydrateLocalAttachments(ctx, msgs, ws, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 {
		t.Fatalf("messages = %d, want 4", len(out))
	}
	if out[3].Role != "user" || len(out[3].Content) != 1 {
		t.Fatalf("vision message = %#v", out[3])
	}
	if out[3].Content[0].Type != "image_url" || out[3].Content[0].Source != "upload/shot.png" {
		t.Fatalf("image part = %#v", out[3].Content[0])
	}
}

func TestPrepareMessagesForLLMDemotesAttachmentsWhenTextOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	msgs := []agentkit.ModelMessage{{
		Role: "user",
		Content: []agentkit.ContentPart{{
			Type:   rtmedia.ContentTypeAttachmentRef,
			Source: "upload/shot.png",
		}},
	}}
	out, err := session.PrepareMessagesForLLM(ctx, msgs, nil, 0, []string{agentkit.ModalityText})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].Content) != 1 {
		t.Fatalf("content = %#v", out[0].Content)
	}
	if out[0].Content[0].Type != "text" || !strings.Contains(out[0].Content[0].Text, "upload/shot.png") {
		t.Fatalf("demoted part = %#v", out[0].Content[0])
	}
}

func TestSanitizeStoresWorkspaceImagePath(t *testing.T) {
	t.Parallel()

	msg := session.SanitizeModelMessageForStorage(agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{{
			Type:   "image_url",
			URL:    "data:image/png;base64,abc",
			Source: "upload/shot.png",
		}},
	}, 0)
	if len(msg.Content) != 1 || msg.Content[0].Type != rtmedia.ContentTypeAttachmentRef {
		t.Fatalf("content = %#v", msg.Content)
	}
}
