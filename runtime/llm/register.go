package llm

import (
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("llm/openai-compatible", NewOpenAI)
	pluginkit.Register("llm/scripted", NewScripted)
	pluginkit.Register("llm/fallback", NewFallback)
}

var (
	_ agentkit.LLMProvider = (*OpenAI)(nil)
	_ agentkit.LLMProvider = (*Scripted)(nil)
	_ agentkit.LLMProvider = (*Fallback)(nil)
)
