package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func newTestFileDeps(t *testing.T, dir string) FileDeps {
	t.Helper()
	ws := rtworkspace.Static(dir)
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: ".", Unrestricted: true}, rtfilesystem.Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	return FileDeps{Workspace: ws, FS: fs}
}

func buildSection(t *testing.T, provider agentkit.SectionProvider) agentkit.PromptSection {
	t.Helper()
	sections := provider.Sections()
	if len(sections) != 1 {
		t.Fatalf("Sections() len = %d, want 1", len(sections))
	}
	section, err := sections[0].Build(context.Background(), agentkit.PromptRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return section
}

func TestFileSectionReadsConfiguredFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "TEAM.md"), []byte("team conventions"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFile(FileConfig{
		Name:      "conventions",
		Filenames: []string{"TEAM.md", "MISSING.md"},
	}, newTestFileDeps(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	section := buildSection(t, provider)
	if section.Name != "conventions" {
		t.Fatalf("section name = %q, want %q", section.Name, "conventions")
	}
	if section.Content != "team conventions" {
		t.Fatalf("content = %q", section.Content)
	}
}

func TestFileSectionDefaultNameAndEmptyContent(t *testing.T) {
	t.Parallel()

	provider, err := NewFile(FileConfig{
		Filenames: []string{"NOPE.md"},
	}, newTestFileDeps(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	section := buildSection(t, provider)
	if section.Name != "file" {
		t.Fatalf("section name = %q, want %q", section.Name, "file")
	}
	if section.Content != "" {
		t.Fatalf("content = %q, want empty", section.Content)
	}
}

func TestFileSectionSkipsEmptyFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "EMPTY.md"), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFile(FileConfig{
		Filenames: []string{"EMPTY.md"},
	}, newTestFileDeps(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if section := buildSection(t, provider); section.Content != "" {
		t.Fatalf("content = %q, want empty", section.Content)
	}
}

func TestFileSectionWalkUpConcatenatesNearestFirst(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	sub := filepath.Join(base, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "TESTWALKUP.md"), []byte("outer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "TESTWALKUP.md"), []byte("inner"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFile(FileConfig{
		Filenames: []string{"TESTWALKUP.md"},
		WalkUp:    true,
	}, newTestFileDeps(t, sub))
	if err != nil {
		t.Fatal(err)
	}
	section := buildSection(t, provider)
	if section.Content != "inner\n\nouter" {
		t.Fatalf("content = %q, want %q", section.Content, "inner\n\nouter")
	}
}

func TestFileSectionNoWalkUpStaysInRoot(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	sub := filepath.Join(base, "a")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "TESTWALKUP.md"), []byte("outer"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFile(FileConfig{
		Filenames: []string{"TESTWALKUP.md"},
	}, newTestFileDeps(t, sub))
	if err != nil {
		t.Fatal(err)
	}
	if section := buildSection(t, provider); section.Content != "" {
		t.Fatalf("content = %q, want empty (no walkUp)", section.Content)
	}
}

func TestNewFileRequiresFilenamesAndDeps(t *testing.T) {
	t.Parallel()

	deps := newTestFileDeps(t, t.TempDir())
	if _, err := NewFile(FileConfig{}, deps); err == nil {
		t.Fatal("expected error for empty filenames")
	}
	if _, err := NewFile(FileConfig{Filenames: []string{"A.md"}}, FileDeps{FS: deps.FS}); err == nil {
		t.Fatal("expected error for missing workspace")
	}
	if _, err := NewFile(FileConfig{Filenames: []string{"A.md"}}, FileDeps{Workspace: deps.Workspace}); err == nil {
		t.Fatal("expected error for missing fs")
	}
}

func TestAgentsMDAliasWalksUpWithDefaultFilenames(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	sub := filepath.Join(base, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "AGENTS.md"), []byte("agent instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider, err := NewAgentsMD(AgentsMDConfig{}, newTestFileDeps(t, sub))
	if err != nil {
		t.Fatal(err)
	}
	section := buildSection(t, provider)
	if section.Name != "agents-md" {
		t.Fatalf("section name = %q, want %q", section.Name, "agents-md")
	}
	if !strings.HasPrefix(section.Content, "agent instructions") {
		t.Fatalf("content = %q, want prefix %q", section.Content, "agent instructions")
	}
}

func TestAgentsMDAliasRespectsRootAndFilenames(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "Automon.md"), []byte("automon persona"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "AGENTS.md"), []byte("should not load"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider, err := NewAgentsMD(AgentsMDConfig{
		Root:      "sub",
		Filenames: []string{"Automon.md"},
	}, newTestFileDeps(t, base))
	if err != nil {
		t.Fatal(err)
	}
	section := buildSection(t, provider)
	// walkUp 从 sub 开始，AGENTS.md 不在 filenames 里，只能命中 sub/Automon.md。
	if section.Content != "automon persona" {
		t.Fatalf("content = %q, want %q", section.Content, "automon persona")
	}
}
