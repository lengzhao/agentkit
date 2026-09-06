package skill_test

import (
	"testing"

	rtskill "github.com/lengzhao/agentkit/runtime/skill"
)

func TestParseDescriptionFrontmatter(t *testing.T) {
	t.Parallel()

	body := `---
name: demo
description: Load when editing MCP servers.
---

# Demo
`
	if got := rtskill.ParseDescription(body); got != "Load when editing MCP servers." {
		t.Fatalf("got %q", got)
	}
}

func TestParseDescriptionMissingFrontmatter(t *testing.T) {
	t.Parallel()

	body := `# Title

One-line summary without frontmatter.
`
	if got := rtskill.ParseDescription(body); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestParseDescriptionMissingDescriptionField(t *testing.T) {
	t.Parallel()

	body := `---
name: broken
---

# Title
`
	if got := rtskill.ParseDescription(body); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
