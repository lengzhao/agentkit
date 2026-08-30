package shell

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("tool/shell-bash", NewShellBash)
}

var _ agentkit.CommandProvider = (*shellBashBundle)(nil)
