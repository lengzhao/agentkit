package acpplatform

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("platform/acp", New)
}

var (
	_ agentkit.Platform  = (*Platform)(nil)
	_ permission.Capable = (*Platform)(nil)
)
