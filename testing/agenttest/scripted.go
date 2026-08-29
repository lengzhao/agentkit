package agenttest

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
)

// MustScripted builds an llm/scripted provider or fails the test.
func MustScripted(t *testing.T, steps ...llm.ScriptedStep) agentkit.LLMProvider {
	t.Helper()
	p, err := llm.NewScripted(llm.ScriptedConfig{Steps: steps})
	if err != nil {
		t.Fatalf("scripted llm: %v", err)
	}
	return p
}
