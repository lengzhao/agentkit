package prompt

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
)

type StaticConfig struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type staticSectionProvider struct {
	name    string
	content string
}

func NewStatic(cfg StaticConfig) (agentkit.SectionProvider, error) {
	name := cfg.Name
	if name == "" {
		name = "static"
	}
	return &staticSectionProvider{
		name:    name,
		content: strings.TrimSpace(cfg.Content),
	}, nil
}

func (p *staticSectionProvider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  p.name,
		Build: p.build,
	}}
}

func (p *staticSectionProvider) build(_ context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	return agentkit.PromptSection{
		Name:    p.name,
		Content: p.content,
	}, nil
}
