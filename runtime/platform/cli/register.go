package cli

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("platform/cli", New)
}

var _ agentkit.Platform = (*Platform)(nil)
