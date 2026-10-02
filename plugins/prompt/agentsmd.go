package prompt

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
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
	// FS reads candidate instruction files; wire an unrestricted filesystem/local
	// instance (the upward walk passes absolute host paths). Non-local backends
	// simply miss every candidate and inject nothing.
	FS filesystem.Service `json:"fs"`
}

type agentsMDProvider struct {
	relRoot   string
	filenames []string
	workspace workspace.Service
	fs        filesystem.Service
}

// SetDefaults implements pluginkit.Defaulter.
func (c *AgentsMDConfig) SetDefaults() {
	if c.Root == "" {
		c.Root = "."
	}
	if len(c.Filenames) == 0 {
		c.Filenames = defaultAgentsMDFilenames()
	}
}

// NewAgentsMD registers prompt/section/agents-md: Inject AGENTS.md instructions discovered in the workspace hierarchy.
func NewAgentsMD(cfg AgentsMDConfig, deps AgentsMDDeps) (agentkit.SectionProvider, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("prompt/section/agents-md requires workspace")
	}
	if deps.FS == nil {
		return nil, fmt.Errorf("prompt/section/agents-md requires fs")
	}
	cfg.SetDefaults()
	return &agentsMDProvider{relRoot: cfg.Root, workspace: deps.Workspace, fs: deps.FS, filenames: cfg.Filenames}, nil
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
			data, err := p.fs.Read(ctx, path)
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
