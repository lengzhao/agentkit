package llm

import (
	"testing"

	"github.com/lengzhao/agentkit"
	openai "github.com/sashabaranov/go-openai"
)

func TestChatStreamFinalizeAfterTrailingUsage(t *testing.T) {
	t.Parallel()

	acc := newStreamAccumulator()
	acc.appendTextDelta("answer")
	acc.setUsage(100, 20, 120)
	acc.finalize()

	var final *agentkit.LLMEvent
	for {
		ev, ok := acc.recvPending()
		if !ok {
			break
		}
		if ev.Type == agentkit.LLMEventMessage {
			copy := ev
			final = &copy
		}
	}
	if final == nil || final.Usage == nil {
		t.Fatal("expected final message with usage")
	}
	if final.Usage.InputTokens != 100 || final.Usage.OutputTokens != 20 || final.Usage.TotalTokens != 120 {
		t.Fatalf("usage = %#v", final.Usage)
	}
}

func TestChatStreamUsageOnlyChunkPattern(t *testing.T) {
	t.Parallel()

	chunk := openai.ChatCompletionStreamResponse{
		Usage: &openai.Usage{
			PromptTokens:     42,
			CompletionTokens: 7,
			TotalTokens:      49,
		},
	}
	if len(chunk.Choices) != 0 {
		t.Fatal("usage-only chunk should have empty choices")
	}
	if chunk.Usage.TotalTokens != 49 {
		t.Fatal("unexpected usage chunk")
	}
}
