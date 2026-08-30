package schedule

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("tool/schedule", NewSchedule)
}

var _ agentkit.CommandProvider = (*scheduleBundle)(nil)
