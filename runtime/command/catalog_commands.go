package command

import (
	"fmt"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type CatalogCommandsConfig struct{}

type CatalogCommandsDeps struct {
	Loop         agentkit.AgentCatalogLoop `json:"loop"`
	SessionStore agentkit.SessionStore     `json:"sessionStore"`
	Workspace    workspace.Service         `json:"workspace"`
	// Subagents (optional) enables /model subagent name validation and listing.
	Subagents capsubagent.Spawner `json:"subagents,omitempty"`
	// LLM (optional, ideally llm/router) enables the model catalog in /model
	// show output and unknown-model warnings on set.
	LLM agentkit.LLMProvider `json:"llm,omitempty"`
}

type catalogCommands struct {
	loop      agentkit.AgentCatalogLoop
	store     agentkit.SessionStore
	workspace workspace.Service
	subagents capsubagent.Spawner
	llm       agentkit.LLMProvider
}

// NewCatalogCommands registers agent/catalog-commands: /agent and /acp slash commands for the loop catalog.
func NewCatalogCommands(_ CatalogCommandsConfig, deps CatalogCommandsDeps) (agentkit.CommandProvider, error) {
	if deps.Loop == nil {
		return nil, fmt.Errorf("agent/catalog-commands requires loop")
	}
	return &catalogCommands{
		loop:      deps.Loop,
		store:     deps.SessionStore,
		workspace: deps.Workspace,
		subagents: deps.Subagents,
		llm:       deps.LLM,
	}, nil
}

func (c *catalogCommands) Commands() []agentkit.Command {
	agents := c.loop.Agents()
	return []agentkit.Command{
		AgentCommand(agents, c.store, c.loop.DefaultAgentID(), c.workspace),
		ModelCommand(agents, c.store, c.loop.DefaultAgentID(), c.workspace, ModelCommandExtra{
			Subagents: c.subagents,
			LLM:       c.llm,
		}),
		ACPCommand(agents, c.store),
	}
}
