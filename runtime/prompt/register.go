package prompt

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("prompt/assembler/default", NewAssembler)
}

var _ agentkit.PromptAssembler = (*Assembler)(nil)
