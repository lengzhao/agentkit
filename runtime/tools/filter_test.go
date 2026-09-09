package tools

import (
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestToolNameFilterAllowList(t *testing.T) {
	t.Parallel()

	f := newToolNameFilter([]string{"read", "grep"}, nil)
	if !f.allows("read") || f.allows("write") {
		t.Fatal("allow list should only permit listed tools")
	}
}

func TestToolNameFilterDenyList(t *testing.T) {
	t.Parallel()

	f := newToolNameFilter(nil, []string{"write", "bash"})
	if f.allows("write") || !f.allows("read") {
		t.Fatal("deny list should block listed tools only")
	}
}

func TestToolNameFilterAllowOverridesDeny(t *testing.T) {
	t.Parallel()

	f := newToolNameFilter([]string{"read"}, []string{"read"})
	if !f.allows("read") {
		t.Fatal("non-empty allow list should take precedence over deny")
	}
}

func TestFilterToolSpecs(t *testing.T) {
	t.Parallel()

	specs := []agentkit.ToolSpec{
		{Name: "read"},
		{Name: "write"},
		{Name: "mcp__ping"},
	}
	filtered := filterToolSpecs(specs, newToolNameFilter(nil, []string{"write"}))
	if len(filtered) != 2 || filtered[0].Name != "read" || filtered[1].Name != "mcp__ping" {
		t.Fatalf("filtered specs = %#v", filtered)
	}
}
