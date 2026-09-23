package llm

import (
	capllm "github.com/lengzhao/agentkit/cap/llm"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("llm/openai-compatible", NewOpenAI)
	pluginkit.Register("llm/openai-chat", NewOpenAIChat)
	pluginkit.Register("llm/openai-responses", NewOpenAIResponses)
	pluginkit.Register("llm/router", NewRouter)
	pluginkit.Register("llm/scripted", NewScripted)
	pluginkit.Register("llm/fallback", NewFallback)
}

var (
	_ agentkit.LLMProvider            = (*OpenAI)(nil)
	_ agentkit.LLMProvider            = (*Scripted)(nil)
	_ agentkit.LLMProvider            = (*Fallback)(nil)
	_ agentkit.LLMProvider            = (*Router)(nil)
	_ agentkit.ModalityAwareLLM       = (*OpenAI)(nil)
	_ agentkit.ModelModalityAwareLLM  = (*OpenAI)(nil)
	_ agentkit.ModelModalityAwareLLM  = (*Router)(nil)
	_ agentkit.ModelModalityAwareLLM  = (*Fallback)(nil)
	_ capllm.ModelCatalog             = (*OpenAI)(nil)
)
