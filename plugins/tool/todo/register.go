package todo

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/todo", NewTodo)
}
