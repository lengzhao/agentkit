package prompt

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

func TestFormatMemoryFileContentStructured(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		"# memory.md",
		"",
		"prefers concise answers",
		"<!-- source=learn created_at=2026-01-01T00:00:00Z -->",
		"",
		"§",
		"",
		"likes Go tests",
	}, "\n")
	got := rtmem.FormatMemoryPromptBody(rtmem.ParseMemory(raw))
	want := "prefers concise answers\n\nlikes Go tests"
	if got != want {
		t.Fatalf("FormatMemoryPromptBody() = %q, want %q", got, want)
	}
}

type stubMemoryReader struct {
	body string
}

func (s stubMemoryReader) LoadEntries(context.Context) ([]capmemory.MemoryEntry, int, int, error) {
	return nil, 0, 0, nil
}

func (s stubMemoryReader) PromptBody(context.Context) (string, error) {
	return s.body, nil
}

func (s stubMemoryReader) PreviewAddOutcome(context.Context, string) (capmemory.AddOutcome, error) {
	return capmemory.AddOutcomeAdded, nil
}

func TestMemoryMDUsesReader(t *testing.T) {
	provider, err := NewMemoryMD(MemoryMDConfig{}, MemoryMDDeps{
		Memory: stubMemoryReader{body: "injected fact"},
	})
	if err != nil {
		t.Fatal(err)
	}
	section, err := provider.Sections()[0].Build(context.Background(), agentkit.PromptRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if section.Content != "injected fact" {
		t.Fatalf("content = %q", section.Content)
	}
}
