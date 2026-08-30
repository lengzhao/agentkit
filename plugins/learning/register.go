package learning

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("learning/default", New)
}

var _ agentkit.CommandProvider = (*Service)(nil)
