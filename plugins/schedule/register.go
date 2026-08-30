package schedule

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("schedule/file", NewFile)
	pluginkit.Register("schedule/multi", NewMulti)
	pluginkit.Register("schedule/cron", NewCron)
}
