package runner

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("runner", New)
}

var _ agentkit.Runner = (*Root)(nil)
var _ agentkit.CommandProvider = (*Root)(nil)
