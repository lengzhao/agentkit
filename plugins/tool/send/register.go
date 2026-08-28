package send

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/send", NewSend)
}
