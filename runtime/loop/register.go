package loop

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("loop/default", New)
}

var (
	_ agentkit.Loop             = (*Default)(nil)
	_ agentkit.CommandProvider  = (*Default)(nil)
)
