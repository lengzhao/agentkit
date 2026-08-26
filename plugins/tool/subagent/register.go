package subagent

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/subagent", NewSubagent)
}
