package send

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("tool/send", NewSend)
}

var _ agentkit.CommandProvider = (*sendBundle)(nil)
