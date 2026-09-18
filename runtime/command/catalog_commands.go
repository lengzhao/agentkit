package command

import (
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type CatalogCommandsConfig struct{}

type CatalogCommandsDeps struct {
	Loop         agentkit.AgentCatalogLoop `json:"loop"`
	SessionStore agentkit.SessionStore     `json:"sessionStore"`
	Workspace    workspace.Service         `json:"workspace"`
}

type catalogCommands struct {
	loop      agentkit.AgentCatalogLoop
	store     agentkit.SessionStore
	workspace workspace.Service
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
	}, nil
}

func (c *catalogCommands) Commands() []agentkit.Command {
	agents := c.loop.Agents()
	return []agentkit.Command{
		AgentCommand(agents, c.store, c.loop.DefaultAgentID(), c.workspace),
		ModelCommand(agents, c.store, c.loop.DefaultAgentID(), c.workspace),
		ACPCommand(agents, c.store),
	}
}
