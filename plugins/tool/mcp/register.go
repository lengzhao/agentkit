package mcp

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/mcp", NewMCP)
}
