package agentsmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/pluginkit"
)

type Config struct {
	Root string `json:"root"`
}

type Deps struct {
	Workspace workspace.Service `json:"workspace"`
}

type Provider struct {
	relRoot   string
	workspace workspace.Service
}

func init() {
	pluginkit.Register("prompt/section/agents-md", New)
}

func New(cfg Config, deps Deps) (agentkit.SectionProvider, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("prompt/section/agents-md requires workspace")
	}
	root := cfg.Root
	if root == "" {
		root = "."
	}
	return &Provider{relRoot: root, workspace: deps.Workspace}, nil
}

func (p *Provider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  "agents-md",
		Order: 10,
		Build: p.build,
	}}
}

func (p *Provider) build(ctx context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	root, err := p.workspace.Resolve(ctx, p.relRoot)
	if err != nil {
		return agentkit.PromptSection{}, err
	}
	var parts []string
	dir := root
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
