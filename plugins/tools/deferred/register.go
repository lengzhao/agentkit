package deferred

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tools/deferred", New)
}
