package llm

import "github.com/lengzhao/agentkit"

// ProviderModalities returns normalized modalities for an LLM provider.
func ProviderModalities(p agentkit.LLMProvider) []string {
	return ProviderModalitiesForModel(p, "")
}

// ProviderModalitiesForModel returns modalities for a specific model when the
// provider implements ModelModalityAwareLLM; otherwise falls back to Modalities().
func ProviderModalitiesForModel(p agentkit.LLMProvider, model string) []string {
	if p == nil {
		return agentkit.NormalizeModalities(nil)
	}
	if aware, ok := p.(agentkit.ModelModalityAwareLLM); ok {
		return agentkit.NormalizeModalities(aware.ModalitiesForModel(model))
	}
	if aware, ok := p.(agentkit.ModalityAwareLLM); ok {
		return agentkit.NormalizeModalities(aware.Modalities())
	}
	return agentkit.NormalizeModalities(nil)
}
