package httphost

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("platform/http", New)
}

var _ agentkit.Platform = (*Platform)(nil)
