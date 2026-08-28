package chatapi

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("platform/chat-api", New)
}

var _ agentkit.Platform = (*Platform)(nil)
