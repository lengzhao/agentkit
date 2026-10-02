package prompt

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
)

type StaticConfig struct {
	// Name is section label, used for ordering and debugging.
	Name string `json:"name"`
	// Content is the prompt text.
	Content string `json:"content"`
}

type staticSectionProvider struct {
	name    string
	content string
}

// NewStatic registers prompt/section/static: Inject a fixed block of system prompt text from config.
// SetDefaults implements pluginkit.Defaulter.
func (c *StaticConfig) SetDefaults() {
	if c.Name == "" {
		c.Name = "static"
	}
}

func NewStatic(cfg StaticConfig) (agentkit.SectionProvider, error) {
	cfg.SetDefaults()
	return &staticSectionProvider{
		name:    cfg.Name,
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
