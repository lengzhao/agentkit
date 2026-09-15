package llm

import "github.com/lengzhao/agentkit"

// ProviderModalities returns normalized modalities for an LLM provider.
func ProviderModalities(p agentkit.LLMProvider) []string {
	if p == nil {
		return agentkit.NormalizeModalities(nil)
	}
	if aware, ok := p.(agentkit.ModalityAwareLLM); ok {
		return agentkit.NormalizeModalities(aware.Modalities())
	}
	return agentkit.NormalizeModalities(nil)
}
