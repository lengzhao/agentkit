package slack

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("platform/slack", New)
}

var _ agentkit.Platform = (*Platform)(nil)
