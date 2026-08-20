package agent

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("agent/coding", New)
}

var _ agentkit.Agent = (*Runtime)(nil)
