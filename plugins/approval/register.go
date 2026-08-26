package approval

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("approval/auto-deny", NewAutoDeny)
	pluginkit.Register("approval/auto-allow", NewAutoAllow)
}
