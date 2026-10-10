package prompt

import (
	"fmt"

	"github.com/lengzhao/agentkit"
)

type AgentsMDConfig struct {
	// Root is directory to start the upward search from.
	Root string `json:"root"`
	// Filenames overrides the default instruction files to search in each directory.
	// Example for Automon runtimes: ["Automon.md", "AUTOMON.md"].
	Filenames []string `json:"filenames"`
}

// AgentsMDDeps is an alias of FileDeps, kept for backward compatibility.
type AgentsMDDeps = FileDeps

// SetDefaults implements pluginkit.Defaulter.
func (c *AgentsMDConfig) SetDefaults() {
	if c.Root == "" {
		c.Root = "."
	}
	if len(c.Filenames) == 0 {
		c.Filenames = defaultAgentsMDFilenames()
	}
}

// NewAgentsMD registers prompt/section/agents-md: an alias of
// prompt/section/file preconfigured to walk up the workspace directory
// hierarchy looking for instruction files (AGENTS.md / CLAUDE.md).
func NewAgentsMD(cfg AgentsMDConfig, deps AgentsMDDeps) (agentkit.SectionProvider, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("prompt/section/agents-md requires workspace")
	}
	if deps.FS == nil {
		return nil, fmt.Errorf("prompt/section/agents-md requires fs")
	}
	cfg.SetDefaults()
	return newFileProvider("agents-md", cfg.Root, cfg.Filenames, true, deps.Workspace, deps.FS), nil
}

func defaultAgentsMDFilenames() []string {
	return []string{"AGENTS.md", "AGENTS.MD", "CLAUDE.md"}
}
