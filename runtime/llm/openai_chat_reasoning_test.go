package llm

import "testing"

func TestChatReasoningEffort(t *testing.T) {
	t.Parallel()

	if got := chatReasoningEffort(nil, 0); got != "" {
		t.Fatalf("no tools: got %q", got)
	}
	if got := chatReasoningEffort(nil, 3); got != "none" {
		t.Fatalf("with tools: got %q", got)
	}
	if got := chatReasoningEffort(&OpenAIReasoningConfig{Effort: "high"}, 3); got != "high" {
		t.Fatalf("explicit effort: got %q", got)
	}
}
