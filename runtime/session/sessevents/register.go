package sessevents

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("session/events", New)
}
