package telemetry

import "github.com/lengzhao/agentkit"

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
