package compaction

import (
	"github.com/lengzhao/agentkit"
	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
)

const defaultCharsPerToken = 4

// estimateMessageChars sizes a message as it appears on the wire: full text of
// every content part plus full part URLs. Inline data: payloads (hydrated
// images) are counted at real size because gateways bill base64 as text;
// underestimating means a provider 400. ContentPart.Source is excluded: it is
// a workspace path for persistence, never sent to providers.
func estimateMessageChars(msg agentkit.ModelMessage) int {
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
	return chars
}

// EstimateTokens approximates message size with a chars/4 heuristic (Pi-compatible).
func EstimateTokens(msg agentkit.ModelMessage) int {
	chars := estimateMessageChars(msg)
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

// indexedMessageTokens sizes an indexed message for cut-point math. The
// ingest-time recorded LogicalChars wins when present: sanitize strips bulky
// parts (attachments) before persistence, so the stored message can be orders
// of magnitude smaller than what hydration later puts on the wire.
func indexedMessageTokens(item capscompaction.IndexedMessage) int {
	if item.LogicalChars > 0 {
		return (item.LogicalChars + defaultCharsPerToken - 1) / defaultCharsPerToken
	}
	return EstimateTokens(item.Message)
}
