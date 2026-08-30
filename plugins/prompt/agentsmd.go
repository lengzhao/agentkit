package prompt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type AgentsMDConfig struct {
	// Root is directory to start the upward search from.
	Root string `json:"root"`
	// Filenames overrides the default instruction files to search in each directory.
	// Example for Automon runtimes: ["Automon.md", "AUTOMON.md"].
	Filenames []string `json:"filenames"`
}

type AgentsMDDeps struct {
	Workspace workspace.Service `json:"workspace"`
}

type agentsMDProvider struct {
	relRoot   string
	filenames []string
	workspace workspace.Service
}

// NewAgentsMD registers prompt/section/agents-md: Inject AGENTS.md instructions discovered in the workspace hierarchy.
func NewAgentsMD(cfg AgentsMDConfig, deps AgentsMDDeps) (agentkit.SectionProvider, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("prompt/section/agents-md requires workspace")
	}
	root := cfg.Root
	if root == "" {
		root = "."
	}
	filenames := cfg.Filenames
	if len(filenames) == 0 {
		filenames = defaultAgentsMDFilenames()
	}
	return &agentsMDProvider{relRoot: root, workspace: deps.Workspace, filenames: filenames}, nil
}

func defaultAgentsMDFilenames() []string {
	return []string{"AGENTS.md", "AGENTS.MD", "CLAUDE.md"}
}

func (p *agentsMDProvider) Sections() []agentkit.Section {
	return []agentkit.Section{{
		Name:  "agents-md",
		Build: p.build,
	}}
}

func (p *agentsMDProvider) build(ctx context.Context, _ agentkit.PromptRequest) (agentkit.PromptSection, error) {
	root, err := p.workspace.Resolve(ctx, p.relRoot)
	if err != nil {
		return agentkit.PromptSection{}, err
	}
	var parts []string
	dir := root
	for {
		for _, name := range p.filenames {
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
