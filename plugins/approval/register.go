package approval

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("approval/cli", NewCLI)
	pluginkit.Register("approval/auto-deny", NewAutoDeny)
}
