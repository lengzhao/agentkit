package tool

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/fs-workspace", NewFSWorkspace)
	pluginkit.Register("tool/fs-memory", NewFSMemory)
	pluginkit.Register("tool/shell-bash", NewShellBash)
	pluginkit.Register("tool/web-fetch-http", NewWebFetchHTTP)
	pluginkit.Register("tool/web-search-exa", NewWebSearchExa)
	pluginkit.Register("tool/web-fetch-scripted", NewWebFetchScripted)
	pluginkit.Register("tool/web-search-scripted", NewWebSearchScripted)
	pluginkit.Register("tool/skill", NewSkill)
	pluginkit.Register("tool/todo", NewTodo)
	pluginkit.Register("tool/finish", NewFinish)
	pluginkit.Register("tool/schedule", NewSchedule)
	pluginkit.Register("tool/subagent", NewSubagent)
	pluginkit.Register("tool/ask-user", NewAskUser)
}
