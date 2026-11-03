package delivery

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("delivery/assistant", NewAssistant)
	pluginkit.Register("delivery/messages", NewMessages)
}
