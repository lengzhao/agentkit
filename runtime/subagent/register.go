package subagent

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("subagent/inprocess", New)
}
