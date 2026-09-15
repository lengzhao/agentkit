package llm

import (
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestOpenAIModalitiesConfig(t *testing.T) {
	t.Parallel()
	p, err := NewOpenAI(OpenAIConfig{
		Model:      "test",
		APIKey:     "k",
		Modalities: []string{"text"},
	}, OpenAIDeps{})
	if err != nil {
		t.Fatal(err)
	}
	aware, ok := p.(agentkit.ModalityAwareLLM)
	if !ok {
		t.Fatal("expected ModalityAwareLLM")
	}
	if agentkit.SupportsModality(aware.Modalities(), agentkit.ModalityImage) {
		t.Fatal("expected text-only")
	}
}
