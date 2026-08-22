package session

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("session/memory", NewMemory)
	pluginkit.Register("session/jsonl", NewJSONL)
	pluginkit.Register("session/store", NewStore)
}

var (
	_ agentkit.Session      = (*Memory)(nil)
	_ agentkit.Session      = (*JSONL)(nil)
	_ agentkit.SessionStore = (*Store)(nil)
)
