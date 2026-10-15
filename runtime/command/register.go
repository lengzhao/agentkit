package command

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("commands/registry", New)
}
