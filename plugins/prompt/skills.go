package prompt

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/skill"
)

type SkillsSectionConfig struct{}

type SkillsSectionDeps struct {
	Skills skill.Registry `json:"skills"`
}

type skillsSectionProvider struct {
	skills skill.Registry
}

func NewSkillsSection(_ SkillsSectionConfig, deps SkillsSectionDeps) (agentkit.SectionProvider, error) {
	if deps.Skills == nil {
		return nil, fmt.Errorf("prompt/section/skills requires skills dependency")
	}
	return &skillsSectionProvider{skills: deps.Skills}, nil
}

func (p *skillsSectionProvider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  "skills",
		Build: p.build,
	}}
}

func (p *skillsSectionProvider) build(ctx context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	list, err := p.skills.List(ctx)
	if err != nil {
		return agentkit.PromptSection{}, err
	}
	if len(list) == 0 {
		return agentkit.PromptSection{}, nil
	}
	var b strings.Builder
	b.WriteString("Available skills (use the skill tool to load one):\n")
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
		Name:    "skills",
		Content: strings.TrimSpace(b.String()),
	}, nil
}
