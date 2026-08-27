package fs

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/filesystem"
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
