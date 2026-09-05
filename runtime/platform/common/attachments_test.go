package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestInboundFromContentSavesFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws := rtworkspace.Static(root)

	files := []FileAttachment{{
		MimeType: "text/plain",
		Data:     []byte("hello attachment"),
		FileName: "note.txt",
	}}
	event := InboundFromContent(
		"assistant",
		session.SessionRouteInput{
			Platform:   "slack",
			DeliveryID: agentkit.SessionID("slack:D0AK8MAHW22:u:U02LNUW8KV5"),
			ScopeUserID: "U02LNUW8KV5",
		},
		"U02LNUW8KV5",
		"附件里面的内容是什么",
		"",
		nil,
		files,
		nil,
		nil,
		InboundOptsFor(ws),
	)

	text := event.Message.Content[0].Text
	if !strings.Contains(text, "附件里面的内容是什么") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "work/"+inboundUploadDir+"/note.txt") {
		t.Fatalf("missing file ref in text: %q", text)
	}

	saved := filepath.Join(root, "work", inboundUploadDir, "note.txt")
	data, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(data) != "hello attachment" {
		t.Fatalf("saved data = %q", data)
	}
}

func TestInboundFromContentSavesImageWorkPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	images := []ImageAttachment{{
		MimeType: "image/png",
		Data:     []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
		FileName: "shot.png",
	}}
	event := InboundFromContent(
		"assistant",
		session.SessionRouteInput{
			Platform:   "slack",
			DeliveryID: agentkit.SessionID("slack:C001"),
			ScopeUserID: "U1",
		},
		"U1",
		"what is this",
		"",
		images,
		nil,
		nil,
		nil,
		InboundOptsFor(ws),
	)
	if len(event.Message.Content) < 2 {
		t.Fatalf("content = %#v", event.Message.Content)
	}
	if event.Message.Content[1].Source == "" {
		t.Fatal("expected workspace source path on image")
	}
}

func TestIsImageAttachment(t *testing.T) {
	t.Parallel()
	if !IsImageAttachment("image/png", "scan.png") {
		t.Fatal("expected png mime to be image")
	}
	if !IsImageAttachment("", "photo.JPG") {
		t.Fatal("expected jpg extension to be image")
	}
	if IsImageAttachment("text/plain", "note.txt") {
		t.Fatal("expected text file not to be image")
	}
}

func TestSanitizeAttachmentFileNameRejectsTraversal(t *testing.T) {
	if got := sanitizeAttachmentFileName("../../escape.txt"); got != "escape.txt" {
		t.Fatalf("basename = %q", got)
	}
	if got := sanitizeAttachmentFileName(".."); got != "" {
		t.Fatalf(".. should be rejected, got %q", got)
	}
}
