package chathistory

import (
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("tool/chat-history", NewChatHistory)
}
