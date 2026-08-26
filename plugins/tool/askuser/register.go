package askuser

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/ask-user", NewAskUser)
}
