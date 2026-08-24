package headless

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("platform/worker", NewWorker)
	pluginkit.Register("platform/timer", NewTimer)
}

var (
	_ agentkit.Platform = (*Worker)(nil)
	_ agentkit.Platform = (*Timer)(nil)
)
