package agent

import (
	"fmt"

	"github.com/lengzhao/agentkit"
)

type CatalogCommandsConfig struct{}

type CatalogCommandsDeps struct {
	Loop         agentkit.AgentCatalogLoop `json:"loop"`
	SessionStore agentkit.SessionStore     `json:"sessionStore"`
}

type catalogCommands struct {
	loop  agentkit.AgentCatalogLoop
	store agentkit.SessionStore
}

// NewCatalogCommands registers agent/catalog-commands: /agent and /acp slash commands for the loop catalog.
func NewCatalogCommands(_ CatalogCommandsConfig, deps CatalogCommandsDeps) (agentkit.CommandProvider, error) {
	if deps.Loop == nil {
		return nil, fmt.Errorf("agent/catalog-commands requires loop")
	}
	return &catalogCommands{
		loop:  deps.Loop,
		store: deps.SessionStore,
	}, nil
}

func (c *catalogCommands) Commands() []agentkit.Command {
	agents := c.loop.Agents()
	return []agentkit.Command{
		Command(agents, c.store, c.loop.DefaultAgentID()),
		ACPCommand(agents, c.store),
	}
}
