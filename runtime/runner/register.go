package runner

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("runner", New)
	pluginkit.Register("platform/cli", NewCLI)
}

var (
	_ agentkit.Runner   = (*Root)(nil)
	_ agentkit.Platform = (*CLI)(nil)
)
