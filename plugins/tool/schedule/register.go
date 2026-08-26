package schedule

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/schedule", NewSchedule)
}
