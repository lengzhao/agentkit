package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestInboundFromContentSavesFiles(t *testing.T) {
	t.Chdir(t.TempDir())

	files := []FileAttachment{{
		MimeType: "text/plain",
		Data:     []byte("hello attachment"),
		FileName: "note.txt",
	}}
	event := InboundFromContent(
		"assistant",
		agentkit.SessionID("slack:D0AK8MAHW22:u:U02LNUW8KV5"),
		"slack",
		"U02LNUW8KV5",
		"附件里面的内容是什么",
		"",
		nil,
		files,
		nil,
		nil,
		nil,
	)

	text := event.Message.Content[0].Text
	if !strings.Contains(text, "附件里面的内容是什么") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, inboundUploadDir+"/note.txt") {
		t.Fatalf("missing file ref in text: %q", text)
	}

	saved := filepath.Join(".agentkit", "slack_D0AK8MAHW22", inboundAttachWorkRoot, inboundUploadDir, "note.txt")
	data, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(data) != "hello attachment" {
		t.Fatalf("saved data = %q", data)
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
