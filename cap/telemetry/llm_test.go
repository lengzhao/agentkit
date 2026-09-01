package telemetry_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/telemetry"
)

func TestLLMTextStarted(t *testing.T) {
	t.Parallel()

	if telemetry.LLMTextStarted(agentkit.LLMEvent{Type: agentkit.AssistantEventTextDelta}) {
		t.Fatal("empty text delta should not count")
	}
	if !telemetry.LLMTextStarted(agentkit.LLMEvent{Type: agentkit.AssistantEventTextDelta, Delta: "hi"}) {
		t.Fatal("text delta should count")
	}
	if !telemetry.LLMTextStarted(agentkit.LLMEvent{Type: agentkit.AssistantEventThinkingDelta, Delta: "hmm"}) {
		t.Fatal("thinking delta should count")
	}
	if telemetry.LLMTextStarted(agentkit.LLMEvent{Type: agentkit.AssistantEventToolCallStart}) {
		t.Fatal("tool call start should not count as text")
	}
}

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
