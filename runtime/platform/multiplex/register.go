package multiplex

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("platform/multiplex", New)
}

var _ agentkit.Platform = (*Platform)(nil)
