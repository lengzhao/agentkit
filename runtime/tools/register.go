package tools

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("tools/runtime", NewRuntime)
}

var _ agentkit.ToolRuntime = (*Runtime)(nil)
