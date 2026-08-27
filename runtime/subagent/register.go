package subagent

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("subagent/inprocess", New)
}

var _ agentkit.CommandProvider = (*Spawner)(nil)
