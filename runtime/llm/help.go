package llm

import "github.com/lengzhao/agentkit/plugindoc"

func init() {
	plugindoc.Register("llm/openai-compatible", plugindoc.Doc{
		Summary: "OpenAI-compatible provider, chat or responses API.",
		ConfigNotes: map[string]string{
			"model":     "model name",
			"baseUrl":   "API base URL, e.g. https://api.openai.com/v1",
			"apiKey":    "inline key. Prefer apiKeyRef so the secret stays out of the config file",
			"apiKeyRef": "credentials reference, e.g. env:OPENAI_API_KEY",
			"api":       "chat or responses",
			"reasoning": "reasoning effort and summary settings, for models that support them",
			"retry":     "provider-level retry, separate from the agent's per-step retry",
		},
		BestPractices: []string{
			"Token budgets and compaction/token-limit need reported usage; the chat API supplies it, the responses API may not.",
		},
	})
	plugindoc.Register("llm/scripted", plugindoc.Doc{
		Summary: "Deterministic canned responses. For tests and offline smoke runs.",
		ConfigNotes: map[string]string{
			"model": "name reported back to callers",
			"steps": "replies in order: text, tool calls, or both",
		},
	})
}
