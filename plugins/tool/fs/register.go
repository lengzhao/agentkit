package fs

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("tool/fs-workspace", NewFSWorkspace)
	pluginkit.Register("tool/fs-memory", NewFSMemory)
}
