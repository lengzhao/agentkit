package telemetry

import "github.com/lengzhao/agentkit"

// LLMTextStarted reports whether ev is the first user-visible text or thinking delta.
func LLMTextStarted(ev agentkit.LLMEvent) bool {
	switch ev.Type {
	case agentkit.AssistantEventTextDelta,
		agentkit.AssistantEventThinkingDelta:
		return ev.Delta != ""
	default:
		return false
	}
}

// LLMCompletionStarted reports whether ev is the first model-produced output
// from an LLM stream: text, thinking, tool call, or a one-shot message.
func LLMCompletionStarted(ev agentkit.LLMEvent) bool {
	switch ev.Type {
	case agentkit.AssistantEventTextDelta,
		agentkit.AssistantEventThinkingDelta,
		agentkit.AssistantEventToolCallStart,
		agentkit.AssistantEventToolCallDelta,
		agentkit.LLMEventMessage:
		return true
	default:
		return false
	}
}
