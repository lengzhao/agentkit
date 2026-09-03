package slack

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/chathistory"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("platform/slack", New)
}

var (
	_ agentkit.Platform    = (*Platform)(nil)
	_ permission.Capable   = (*Platform)(nil)
	_ chathistory.Provider = (*Platform)(nil)
)
