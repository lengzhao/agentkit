package tool

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/read-file", NewReadFile)
	pluginkit.Register("tool/write-file", NewWriteFile)
	pluginkit.Register("tool/edit-file", NewEditFile)
	pluginkit.Register("tool/grep", NewGrep)
	pluginkit.Register("tool/find", NewFind)
	pluginkit.Register("tool/list-dir", NewListDir)
	pluginkit.Register("tool/shell", NewShell)
	pluginkit.Register("tool/skill", NewSkill)
	pluginkit.Register("tool/todo", NewTodo)
	pluginkit.Register("tool/finish", NewFinish)
	pluginkit.Register("tool/schedule", NewSchedule)
	pluginkit.Register("tool/subagent", NewSubagent)
	pluginkit.Register("tool/web-fetch", NewWebFetch)
	pluginkit.Register("tool/web-search", NewWebSearch)
	pluginkit.Register("tool/ask-user", NewAskUser)
}
