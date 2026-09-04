package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
	"github.com/lengzhao/agentkit/cap/workspace"
)

func TestSliceReadContentOffsetLimit(t *testing.T) {
	t.Parallel()
	content := "a\nb\nc\nd\ne"
	got, err := sliceReadContent(content, readSliceOptions{Offset: 2, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "b\nc" {
		t.Fatalf("content = %q", got.Content)
	}
	if got.Hint == "" {
		t.Fatal("expected continuation hint")
	}
}

func TestFormatReadTextIncludesLineNumbersAndHint(t *testing.T) {
	t.Parallel()
	sliced, err := sliceReadContent("alpha\nbeta", readSliceOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	text := formatReadText("main.go", 1, sliced)
	if !strings.Contains(text, "main.go\n") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "     1|alpha") || !strings.Contains(text, "     2|beta") {
		t.Fatalf("text = %q", text)
	}
}

func TestFormatGrepResultEmpty(t *testing.T) {
	t.Parallel()
	if got := formatGrepResult(filesystem.GrepResult{}); got != "No matches found" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatFindResultWithHint(t *testing.T) {
	t.Parallel()
	got := formatFindResult(filesystem.FindResult{
		Paths:     []string{"a.go"},
		Text:      "a.go",
		Truncated: true,
		Hint:      "limit reached",
	})
	if !strings.Contains(got, "a.go") || !strings.Contains(got, "limit reached") {
		t.Fatalf("got %q", got)
	}
}

func TestApplyEditsOnOriginalOverlap(t *testing.T) {
	t.Parallel()
	_, err := applyEditsOnOriginal("abcdef", []FileEdit{
		{OldText: "bc", NewText: "X"},
		{OldText: "cd", NewText: "Y"},
	})
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("expected overlap error, got %v", err)
	}
}

func TestApplyEditsOnOriginalIndependent(t *testing.T) {
	t.Parallel()
	out, err := applyEditsOnOriginal("alpha beta gamma", []FileEdit{
		{OldText: "alpha", NewText: "A"},
		{OldText: "gamma", NewText: "G"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "A beta G" {
		t.Fatalf("got %q", out)
	}
}

func TestMatchFilePatternRecursive(t *testing.T) {
	t.Parallel()
	ok, err := matchFilePattern("**/*.go", "pkg/sub/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected match")
	}
}

func TestGrepCollectorContext(t *testing.T) {
	t.Parallel()
	lines := []string{"before", "match", "after"}
	c := newGrepCollector(10)
	if !c.addMatch("a.go", 2, lines, 1) {
		t.Fatal("addMatch failed")
	}
	result := c.Result()
	if !strings.Contains(result.Text, "a.go-1- before") {
		t.Fatalf("text = %q", result.Text)
	}
	if !strings.Contains(result.Text, "a.go:2: match") {
		t.Fatalf("text = %q", result.Text)
	}
}

func TestWorkspaceFSRootIsTenantLocal(t *testing.T) {
	t.Parallel()

	tenantRoot := t.TempDir()
	workDir := filepath.Join(tenantRoot, "work")
	skillDir := filepath.Join(tenantRoot, "skills", "demo")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := workspace.Static(tenantRoot)
	fs := &workspaceFS{relRoot: ".", workspace: ws}
	ctx := context.Background()

	if err := fs.writeText(ctx, "AGENTS.md", "tenant instructions"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(tenantRoot, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "tenant instructions" {
		t.Fatalf("AGENTS.md = %q", got)
	}

	if err := fs.writeText(ctx, "skills/demo/reference.md", "# ref"); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(skillDir, "reference.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# ref" {
		t.Fatalf("skills file = %q", got)
	}

	if err := fs.writeText(ctx, "work/notes.txt", "temp"); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(workDir, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "temp" {
		t.Fatalf("notes.txt = %q", got)
	}
}

func TestWorkspaceFSRejectsPathEscapeByDefault(t *testing.T) {
	t.Parallel()

	tenantRoot := t.TempDir()
	workDir := filepath.Join(tenantRoot, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parentFile := filepath.Join(tenantRoot, "secret.txt")
	if err := os.WriteFile(parentFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := workspace.Static(tenantRoot)
	fs := &workspaceFS{relRoot: "work", workspace: ws}
	ctx := context.Background()

	if _, err := fs.readText(ctx, "../secret.txt", 0); err == nil {
		t.Fatal("expected path escape error")
	}
	if err := fs.writeText(ctx, "../escape.txt", "bad"); err == nil {
		t.Fatal("expected path escape error on write")
	}
}

func TestWorkspaceFSUnrestrictedPaths(t *testing.T) {
	t.Parallel()

	tenantRoot := t.TempDir()
	workDir := filepath.Join(tenantRoot, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parentFile := filepath.Join(tenantRoot, "secret.txt")
	if err := os.WriteFile(parentFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := workspace.Static(tenantRoot)
	fs := &workspaceFS{relRoot: "work", workspace: ws, unrestricted: true}
	ctx := context.Background()

	got, err := fs.readText(ctx, "../secret.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret" {
		t.Fatalf("secret.txt = %q", got)
	}
	if err := fs.writeText(ctx, "../escape.txt", "ok"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(tenantRoot, "escape.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "ok" {
		t.Fatalf("escape.txt = %q", string(raw))
	}

	absFile := filepath.Join(tenantRoot, "abs.txt")
	if err := os.WriteFile(absFile, []byte("abs"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = fs.readText(ctx, absFile, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != "abs" {
		t.Fatalf("abs file = %q", got)
	}
}
