package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestReadSessionFileLargeLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "large.jsonl")
	large := strings.Repeat("A", 200*1024)
	line := `{"Seq":1,"Type":"user/message","Data":{"Role":"user","Content":[{"type":"image_url","url":"data:image/png;base64,` + large + `"}]}}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	events, err := readSessionFile(path, 0)
	if err != nil {
		t.Fatalf("readSessionFile: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Type != agentkit.EventUserMessage {
		t.Fatalf("type = %q", events[0].Type)
	}
}

func TestScanSessionFileLargeLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "large.jsonl")
	large := strings.Repeat("B", 200*1024)
	line := `{"Seq":1,"Type":"user/message","Data":{"Role":"user","Content":[{"type":"text","text":"` + large + `"}]}}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	events, maxSeq, trimmed, err := scanSessionFile(path, 0)
	if err != nil {
		t.Fatalf("scanSessionFile: %v", err)
	}
	if len(events) != 1 || maxSeq != 1 || trimmed {
		t.Fatalf("events=%d maxSeq=%d trimmed=%v", len(events), maxSeq, trimmed)
	}
}
