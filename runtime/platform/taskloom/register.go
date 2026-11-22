package taskloom

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("platform/taskloom", New)
}

var (
	_ agentkit.Platform           = (*Platform)(nil)
	_ agentkit.PlatformIdentifier = (*Platform)(nil)
)
