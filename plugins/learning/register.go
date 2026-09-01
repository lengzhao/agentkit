package learning

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("learning/default", New)
	pluginkit.Register("learning/dream-sweep", NewDreamSweep)
}

var _ agentkit.CommandProvider = (*Service)(nil)
