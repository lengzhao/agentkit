package markdown_test

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/runtime/markdown"
)

func TestSplitValidFrontmatter(t *testing.T) {
	t.Parallel()

	raw := "---\nname: demo\ndescription: Demo skill\n---\n\nBody text.\n"
	yaml, body, ok := markdown.Split(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(yaml, "name: demo") {
		t.Fatalf("yaml = %q", yaml)
	}
	if body != "\nBody text.\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestSplitCRLF(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		"---",
		"name: crlf",
		"description: CRLF skill",
		"---",
		"",
		"CRLF body.",
	}, "\r\n")
	_, body, ok := markdown.Split(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if strings.TrimSpace(body) != "CRLF body." {
		t.Fatalf("body = %q", body)
	}
}

func TestSplitMissingFrontmatter(t *testing.T) {
	t.Parallel()

	_, body, ok := markdown.Split("No frontmatter.")
	if ok {
		t.Fatal("expected not ok")
	}
	if body != "No frontmatter." {
		t.Fatalf("body = %q", body)
	}
}

func TestSplitUnterminatedFrontmatter(t *testing.T) {
	t.Parallel()

	_, body, ok := markdown.Split("---\nname: open\n")
	if ok {
		t.Fatal("expected not ok")
	}
	if body != "---\nname: open\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestSplitDelimiterInsideYAMLValue(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		"---",
		"name: block-skill",
		"description: |",
		"  Includes a ---- marker that is not a delimiter.",
		"---",
		"",
		"Block body.",
	}, "\n")
	yaml, body, ok := markdown.Split(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(yaml, "---- marker") {
		t.Fatalf("yaml = %q", yaml)
	}
	if strings.TrimSpace(body) != "Block body." {
		t.Fatalf("body = %q", body)
	}
}

func TestSplitIgnoresExtraDelimitersInBody(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		"---",
		"name: demo",
		"description: Demo skill",
		"---",
		"",
		"Section one.",
		"----",
		"Section two.",
		"---",
		"Section three.",
	}, "\n")
	yaml, body, ok := markdown.Split(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(yaml, "name: demo") {
		t.Fatalf("yaml = %q", yaml)
	}
	if !strings.Contains(body, "----") {
		t.Fatalf("body should preserve ----, got %q", body)
	}
	if strings.Count(body, "---") != 2 {
		t.Fatalf("body should preserve trailing --- lines, got %q", body)
	}
	if !strings.Contains(body, "Section three.") {
		t.Fatalf("body = %q", body)
	}
}
