package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/learning"
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
	got := formatMemoryFileContent([]byte(raw))
	want := "prefers concise answers\n\nlikes Go tests"
	if got != want {
		t.Fatalf("formatMemoryFileContent() = %q, want %q", got, want)
	}
}

func TestMemoryMDBuildStripsMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	body := learning.RenderMemory([]learning.MemoryEntry{{
		Content: "remember this",
		Meta:    "source=test",
	}})
	if err := os.WriteFile(filepath.Join(work, "memory.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	svc, err := learning.New(learning.Config{}, learning.Deps{
		Workspace:    workspace.Static(root),
		SessionStore: learningStubSessionStore{},
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewMemoryMD(MemoryMDConfig{Root: "work"}, MemoryMDDeps{
		Workspace: workspace.Static(root),
		Learning:  svc,
	})
	if err != nil {
		t.Fatal(err)
	}

	section, err := provider.Sections()[0].Build(t.Context(), agentkit.PromptRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if section.Content != "remember this" {
		t.Fatalf("content = %q", section.Content)
	}
	if strings.Contains(section.Content, "<!--") || strings.Contains(section.Content, "# memory.md") {
		t.Fatalf("metadata leaked into prompt: %q", section.Content)
	}
}

type learningStubSessionStore struct{}

func (learningStubSessionStore) Get(context.Context, agentkit.SessionID) (agentkit.Session, error) {
	return nil, nil
}
