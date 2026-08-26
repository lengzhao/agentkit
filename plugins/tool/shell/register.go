package shell

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/shell-bash", NewShellBash)
}
