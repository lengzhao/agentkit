package sessstore

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

	events, maxSeq, trimmed, err := ScanSessionFile(path, 0)
	if err != nil {
		t.Fatalf("scanSessionFile: %v", err)
	}
	if len(events) != 1 || maxSeq != 1 || trimmed {
		t.Fatalf("events=%d maxSeq=%d trimmed=%v", len(events), maxSeq, trimmed)
	}
}

// TestReadSessionFileOversizedLine verifies the loader no longer fails on a
// single JSONL record larger than the old bufio.Scanner 1 MiB line cap. This
// reproduces the "bufio.Scanner: token too long" failure seen on long sessions
// and /compact once history accumulates large images or tool outputs.
func TestReadSessionFileOversizedLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "oversized.jsonl")
	// 2 MiB of payload -> the whole line is well above the former 1 MiB cap.
	large := strings.Repeat("C", 2<<20)
	line := `{"Seq":1,"Type":"user/message","Data":{"Role":"user","Content":[{"type":"image_url","url":"data:image/png;base64,` + large + `"}]}}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	events, err := readSessionFile(path, 0)
	if err != nil {
		t.Fatalf("readSessionFile oversized line: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Seq != 1 {
		t.Fatalf("seq = %d, want 1", events[0].Seq)
	}
}

func TestScanSessionFileOversizedLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "oversized.jsonl")
	large := strings.Repeat("D", 2<<20)
	line := `{"Seq":7,"Type":"user/message","Data":{"Role":"user","Content":[{"type":"text","text":"` + large + `"}]}}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	events, maxSeq, trimmed, err := ScanSessionFile(path, 0)
	if err != nil {
		t.Fatalf("ScanSessionFile oversized line: %v", err)
	}
	if len(events) != 1 || maxSeq != 7 || trimmed {
		t.Fatalf("events=%d maxSeq=%d trimmed=%v", len(events), maxSeq, trimmed)
	}
}

// TestScanSessionFileMultipleOversizedLines ensures streaming decode handles a
// mix of small and oversized records without truncating later records.
func TestScanSessionFileMultipleOversizedLines(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")
	small := `{"Seq":1,"Type":"user/message","Data":{"Role":"user","Content":[{"type":"text","text":"hi"}]}}`
	large := strings.Repeat("E", 2<<20)
	big := `{"Seq":2,"Type":"assistant/message","Data":{"Role":"assistant","Content":[{"type":"text","text":"` + large + `"}]}}`
	tail := `{"Seq":3,"Type":"user/message","Data":{"Role":"user","Content":[{"type":"text","text":"bye"}]}}`
	content := small + "\n" + big + "\n" + tail + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	events, maxSeq, trimmed, err := ScanSessionFile(path, 0)
	if err != nil {
		t.Fatalf("ScanSessionFile mixed: %v", err)
	}
	if len(events) != 3 || maxSeq != 3 || trimmed {
		t.Fatalf("events=%d maxSeq=%d trimmed=%v", len(events), maxSeq, trimmed)
	}
	if events[2].Seq != 3 {
		t.Fatalf("tail seq = %d, want 3", events[2].Seq)
	}
}
