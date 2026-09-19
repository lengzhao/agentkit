package compaction

import (
	"github.com/lengzhao/agentkit"
)

const defaultCharsPerToken = 4

// EstimateTokens approximates message size with a chars/4 heuristic (Pi-compatible).
func EstimateTokens(msg agentkit.ModelMessage) int {
	chars := len(msg.Role)
	for _, part := range msg.Content {
		if part.Type == "text" {
			chars += len(part.Text)
		}
	}
	for _, call := range msg.ToolCalls {
		chars += len(call.Name) + len(call.Input)
	}
	for _, result := range msg.ToolResults {
		chars += len(result.Name) + len(result.Content)
	}
	if chars == 0 {
		return 0
	}
	return (chars + defaultCharsPerToken - 1) / defaultCharsPerToken
}

// EstimateMessagesTokens sums token estimates for a message slice.
func EstimateMessagesTokens(messages []agentkit.ModelMessage) int {
	total := 0
	for _, msg := range messages {
		total += EstimateTokens(msg)
	}
	return total
}

// EstimateSendTokens approximates request size as actually sent: full text plus
// full part URLs (inline data: payloads counted at real size), unlike
// EstimateTokens which ignores non-text parts. The pre-send guard uses it
// because underestimating there means a provider 400. ContentPart.Source is
// excluded: it is a workspace path for persistence, never sent to providers.
func EstimateSendTokens(msg agentkit.ModelMessage) int {
	chars := len(msg.Role)
	for _, part := range msg.Content {
		chars += len(part.Text) + len(part.URL)
	}
	for _, call := range msg.ToolCalls {
		chars += len(call.Name) + len(call.Input)
	}
	for _, result := range msg.ToolResults {
		chars += len(result.Name) + len(result.Content)
	}
	if chars == 0 {
		return 0
	}
	return (chars + defaultCharsPerToken - 1) / defaultCharsPerToken
}

// EstimateMessagesSendTokens sums send-size estimates for a message slice.
func EstimateMessagesSendTokens(messages []agentkit.ModelMessage) int {
	total := 0
	for _, msg := range messages {
		total += EstimateSendTokens(msg)
	}
	return total
}
