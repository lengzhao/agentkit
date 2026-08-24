package fs

import "github.com/lengzhao/pluginkit"

func init() {
	pluginkit.Register("fs/local", NewLocal)
	pluginkit.Register("fs/memory", NewMemory)
	pluginkit.Register("fs/readonly", NewReadonly)
}
