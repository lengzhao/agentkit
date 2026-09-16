package llm_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
)

func TestCompleteTextScripted(t *testing.T) {
	t.Parallel()

	provider, err := llm.NewScripted(llm.ScriptedConfig{
		Steps: []llm.ScriptedStep{{Text: "hello vision"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := llm.CompleteText(context.Background(), provider, agentkit.LLMRequest{
		Messages: []agentkit.ModelMessage{
			{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "ping"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello vision" {
		t.Fatalf("text = %q", text)
	}
}
