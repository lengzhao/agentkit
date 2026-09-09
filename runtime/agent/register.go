package agent

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("agent/coding", New)
	pluginkit.Register("agent/catalog-commands", NewCatalogCommands)
}

var (
	_ agentkit.Agent             = (*Runtime)(nil)
	_ agentkit.AgentCatalogEntry = (*Runtime)(nil)
	_ agentkit.CommandProvider   = (*catalogCommands)(nil)
)
