package prompt

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
)

type SubagentsSectionConfig struct{}

type SubagentsSectionDeps struct {
	Subagent subagent.Spawner `json:"subagent"`
}

type subagentsSectionProvider struct {
	spawner subagent.Spawner
}

// NewSubagentsSection injects the delegatable agent catalog. It belongs in the
// prompt rather than in the delegate tool's description because definitions are
// read from disk and can change between turns, while a tool description is fixed
// when the tool is built.
func NewSubagentsSection(_ SubagentsSectionConfig, deps SubagentsSectionDeps) (agentkit.SectionProvider, error) {
	if deps.Subagent == nil {
		return nil, fmt.Errorf("prompt/section/subagents requires subagent dependency")
	}
	return &subagentsSectionProvider{spawner: deps.Subagent}, nil
}

func (p *subagentsSectionProvider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  "subagents",
		Build: p.build,
	}}
}

func (p *subagentsSectionProvider) build(ctx context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	list, err := p.spawner.Definitions(ctx)
	if err != nil {
		return agentkit.PromptSection{}, err
	}
	if len(list) == 0 {
		return agentkit.PromptSection{}, nil
	}
	var b strings.Builder
	b.WriteString("Available subagents (use the delegate tool to run one):\n")
	for _, item := range list {
		b.WriteString("- ")
		b.WriteString(item.Name)
		if item.Description != "" {
			b.WriteString(": ")
			b.WriteString(item.Description)
		}
		b.WriteString("\n")
	}
	return agentkit.PromptSection{
		Name:    "subagents",
		Content: strings.TrimSpace(b.String()),
	}, nil
}
