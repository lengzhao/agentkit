package memory

import (
	"github.com/lengzhao/agentkit"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("memory/default", New)
}

var (
	_ capmemory.Service                = (*Service)(nil)
	_ capmemory.Tool                   = (*Service)(nil)
	_ capmemory.CommitObserverRegistrar = (*Service)(nil)
	_ agentkit.CommandProvider         = (*Service)(nil)
)
