package agentsmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	Root string `json:"root"`
}

type Provider struct {
	root string
}

func init() {
	pluginkit.Register("prompt/section/agents-md", New)
}

func New(cfg Config) (agentkit.SectionProvider, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Provider{root: abs}, nil
}

func (p *Provider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  "agents-md",
		Order: 10,
		Build: p.build,
	}}
}

func (p *Provider) build(_ context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	var parts []string
	dir := p.root
	for {
		for _, name := range []string{"AGENTS.md", "AGENTS.MD", "CLAUDE.md"} {
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err == nil && len(strings.TrimSpace(string(data))) > 0 {
				parts = append(parts, string(data))
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return agentkit.PromptSection{
		Name:    "agents-md",
		Content: strings.Join(parts, "\n\n"),
	}, nil
}
