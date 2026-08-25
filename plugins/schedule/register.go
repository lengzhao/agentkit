package schedule

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("schedule/file", NewFile)
}
