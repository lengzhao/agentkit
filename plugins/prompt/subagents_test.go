package prompt_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/plugins/prompt"
)

type stubSpawner struct {
	defs []subagent.Definition
}

func (s stubSpawner) Definitions(context.Context) ([]subagent.Definition, error) {
	return s.defs, nil
}

func (s stubSpawner) Run(context.Context, subagent.Request) (subagent.Result, error) {
	return subagent.Result{}, nil
}

func buildSection(t *testing.T, spawner subagent.Spawner) agentkit.PromptSection {
	t.Helper()
	provider, err := prompt.NewSubagentsSection(prompt.SubagentsSectionConfig{}, prompt.SubagentsSectionDeps{
		Subagent: spawner,
	})
	if err != nil {
		t.Fatal(err)
	}
	sections := provider.Sections()
	if len(sections) != 1 || sections[0].Name != "subagents" {
		t.Fatalf("sections = %+v, want one named subagents", sections)
	}
	section, err := sections[0].Build(context.Background(), agentkit.PromptRequest{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return section
}

func TestSubagentsSectionListsNamesAndDescriptions(t *testing.T) {
	t.Parallel()

	section := buildSection(t, stubSpawner{defs: []subagent.Definition{
		{Name: "researcher", Description: "read-only research"},
		{Name: "reviewer", Description: "reviews code"},
	}})

	for _, want := range []string{"researcher: read-only research", "reviewer: reviews code", "delegate"} {
		if !strings.Contains(section.Content, want) {
			t.Errorf("section content missing %q:\n%s", want, section.Content)
		}
	}
}

func TestSubagentsSectionEmptyWhenNoDefinitions(t *testing.T) {
	t.Parallel()

	// An empty section is dropped by the assembler, so a project with no agents/
	// directory pays nothing for having the section wired.
	if section := buildSection(t, stubSpawner{}); section.Content != "" {
		t.Fatalf("content = %q, want empty", section.Content)
	}
}

func TestSubagentsSectionRequiresSpawner(t *testing.T) {
	t.Parallel()

	if _, err := prompt.NewSubagentsSection(prompt.SubagentsSectionConfig{}, prompt.SubagentsSectionDeps{}); err == nil {
		t.Fatal("expected an error without a subagent dependency")
	}
}
