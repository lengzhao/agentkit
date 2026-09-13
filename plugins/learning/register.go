package learning

import (
	"github.com/lengzhao/agentkit"
	caplearning "github.com/lengzhao/agentkit/cap/learning"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("learning/default", New)
	pluginkit.Register("learning/dream-sweep", NewDreamSweep)
}

var (
	_ agentkit.CommandProvider   = (*Service)(nil)
	_ capmemory.CommitObserver   = (*Service)(nil)
	_ caplearning.SkillProposer  = (*Service)(nil)
)
