package sessstore

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("session/memory", NewMemory)
	pluginkit.Register("session/jsonl", NewJSONL)
	pluginkit.Register("session/store", NewStore)
	pluginkit.Register("session/static", NewStatic)
	pluginkit.Register("session/sql", NewSQLStore)
	pluginkit.Register("session/sqlite", NewSQLiteStore)
	pluginkit.Register("session/postgres", NewPostgresStore)
	pluginkit.Register("session/commands", NewCommands)
}

var (
	_ agentkit.Session         = (*Memory)(nil)
	_ agentkit.Session         = (*JSONL)(nil)
	_ agentkit.Session         = (*dbSession)(nil)
	_ agentkit.SessionStore    = (*Store)(nil)
	_ agentkit.SessionStore    = (*StaticStore)(nil)
	_ agentkit.SessionStore         = (*SQLStore)(nil)
	_ agentkit.ActiveSessionStore   = (*SQLStore)(nil)
	_ agentkit.SessionRuntimeStore  = (*SQLStore)(nil)
	_ agentkit.CommandProvider = (*Commands)(nil)
)
