package telemetry_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/telemetry"
)

func TestLLMCompletionStarted(t *testing.T) {
	t.Parallel()

	startEvents := []agentkit.AssistantMessageEventType{
		agentkit.AssistantEventStart,
		agentkit.AssistantEventTextStart,
		agentkit.AssistantEventThinkingStart,
		agentkit.AssistantEventTextEnd,
		agentkit.AssistantEventToolCallEnd,
	}
	for _, typ := range startEvents {
		if telemetry.LLMCompletionStarted(agentkit.LLMEvent{Type: typ}) {
			t.Fatalf("event %q should not mark completion start", typ)
		}
	}

	completionEvents := []agentkit.AssistantMessageEventType{
		agentkit.AssistantEventTextDelta,
		agentkit.AssistantEventThinkingDelta,
		agentkit.AssistantEventToolCallStart,
		agentkit.AssistantEventToolCallDelta,
		agentkit.LLMEventMessage,
	}
	for _, typ := range completionEvents {
		if !telemetry.LLMCompletionStarted(agentkit.LLMEvent{Type: typ}) {
			t.Fatalf("event %q should mark completion start", typ)
		}
	}
}
