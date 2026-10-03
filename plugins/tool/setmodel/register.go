package setmodel

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/set-model", New)
}
