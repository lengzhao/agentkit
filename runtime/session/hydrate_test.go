package session_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
	"github.com/lengzhao/agentkit/runtime/session"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
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
	if len(out) != 3 {
		t.Fatalf("messages = %d, want 3", len(out))
	}
	if out[0].Content[1].Type != "image_url" || out[0].Content[1].URL == "" {
		t.Fatalf("image part = %#v", out[0].Content[1])
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
	if len(out) != 3 {
		t.Fatalf("messages = %d, want 3", len(out))
	}
	if out[0].Role != "user" || len(out[0].Content) != 2 {
		t.Fatalf("user message = %#v", out[0].Content)
	}
	if out[0].Content[1].Type != "image_url" || out[0].Content[1].Source != "upload/shot.png" {
		t.Fatalf("image part = %#v", out[0].Content[1])
	}
}

func TestHydrateLocalAttachmentsFitsLargeWorkspaceImage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workDir := filepath.Join(root, "work", "upload")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 3200, 2400))
	for y := 0; y < 2400; y++ {
		for x := 0; x < 3200; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "big.jpg"), raw.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := rtworkspace.Static(root)
	ctx := context.Background()
	msgs := []agentkit.ModelMessage{{
		Role: "user",
		Content: []agentkit.ContentPart{{
			Type:   rtmedia.ContentTypeAttachmentRef,
			Source: "upload/big.jpg",
			MIME:   "image/jpeg",
		}},
	}}
	out, err := session.HydrateLocalAttachments(ctx, msgs, ws, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].Content) != 1 {
		t.Fatalf("content = %#v", out[0].Content)
	}
	part := out[0].Content[0]
	if part.Type != "image_url" || part.URL == "" {
		t.Fatalf("image part = %#v", part)
	}
	const prefix = "base64,"
	idx := strings.Index(part.URL, prefix)
	if idx < 0 {
		t.Fatalf("url missing base64 payload: %q", part.URL)
	}
	payload, err := base64.StdEncoding.DecodeString(part.URL[idx+len(prefix):])
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > rtmedia.DefaultMaxVisionPayloadBytes {
		t.Fatalf("payload %d exceeds %d", len(payload), rtmedia.DefaultMaxVisionPayloadBytes)
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

	ws := rtworkspace.Static(t.TempDir())
	msg := session.SanitizeModelMessageForStorageWS(agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{{
			Type:   "image_url",
			URL:    "data:image/png;base64,abc",
			Source: "upload/shot.png",
		}},
	}, 0, ws)
	if len(msg.Content) != 1 || msg.Content[0].Type != rtmedia.ContentTypeAttachmentRef {
		t.Fatalf("content = %#v", msg.Content)
	}
	if msg.Content[0].Source != "upload/shot.png" {
		t.Fatalf("Source = %q", msg.Content[0].Source)
	}
}
