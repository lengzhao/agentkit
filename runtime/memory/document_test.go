package memory

import (
	"strings"
	"testing"
)

func TestRenderMemoryOmitsSourceComments(t *testing.T) {
	body := RenderMemory([]MemoryEntry{{
		Content: "likes tea",
		Meta:    "source=test created_at=2026-01-01T00:00:00Z",
	}})
	if strings.Contains(body, "<!--") {
		t.Fatalf("render leaked meta: %q", body)
	}
	if !strings.Contains(body, "likes tea") {
		t.Fatalf("render = %q", body)
	}
}

func TestFormatMemoryPromptBody(t *testing.T) {
	got := FormatMemoryPromptBody([]MemoryEntry{
		{Content: "a"},
		{Content: "a"},
		{Content: "longer than a"},
	})
	if got != "longer than a" {
		t.Fatalf("deduped body = %q", got)
	}
}

func TestParseMemoryStripsLegacyComments(t *testing.T) {
	raw := "# memory.md\n\nfact one\n<!-- source=old -->\n§\nfact two\n"
	entries := ParseMemory(raw)
	if len(entries) != 2 || entries[0].Content != "fact one" {
		t.Fatalf("entries = %#v", entries)
	}
}
