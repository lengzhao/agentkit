package telemetry

import (
	"encoding/json"

	"github.com/lengzhao/agentkit"
)

// ExportMessages serializes the assembled prompt for telemetry handoff without
// truncation or prefix deduplication.
func ExportMessages(messages []agentkit.ModelMessage) string {
	if len(messages) == 0 {
		return ""
	}
	entries := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		entries = append(entries, messageEntryForExport(msg, 0))
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return ""
	}
	return string(raw)
}

// FormatGenerationInputForExport builds Langfuse-friendly generation input:
// valid JSON wrapper, per-field truncation, optional shared-prefix dedup.
func FormatGenerationInputForExport(prev, cur []agentkit.ModelMessage, maxFieldBytes int, dedupePrefix bool) string {
	if len(cur) == 0 {
		return ""
	}
	sharedPrefix := 0
	if dedupePrefix && len(prev) > 0 {
		sharedPrefix = messagesSharedPrefix(prev, cur)
	}
	return marshalGenerationExport(sharedPrefix, cur[sharedPrefix:], maxFieldBytes)
}

func marshalGenerationExport(sharedPrefix int, messages []agentkit.ModelMessage, maxFieldBytes int) string {
	summaries := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		summaries = append(summaries, messageEntryForExport(msg, maxFieldBytes))
	}
	payload := map[string]any{
		"sharedPrefixMessages": sharedPrefix,
		"messages":             summaries,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}

func messagesSharedPrefix(prev, cur []agentkit.ModelMessage) int {
	n := 0
	for i := 0; i < len(prev) && i < len(cur); i++ {
		if !messagesEqual(prev[i], cur[i]) {
			break
		}
		n++
	}
	return n
}

func messagesEqual(a, b agentkit.ModelMessage) bool {
	if a.Role != b.Role {
		return false
	}
	if len(a.Content) != len(b.Content) {
		return false
	}
	for i := range a.Content {
		if !contentPartsEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}
	if len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		ca, cb := a.ToolCalls[i], b.ToolCalls[i]
		if ca.ID != cb.ID || ca.Name != cb.Name || string(ca.Input) != string(cb.Input) {
			return false
		}
	}
	if len(a.ToolResults) != len(b.ToolResults) {
		return false
	}
	for i := range a.ToolResults {
		ra, rb := a.ToolResults[i], b.ToolResults[i]
		if ra.ID != rb.ID || ra.Name != rb.Name || ra.Content != rb.Content {
			return false
		}
	}
	return true
}

func contentPartsEqual(a, b agentkit.ContentPart) bool {
	return a.Type == b.Type &&
		a.Text == b.Text &&
		a.URL == b.URL &&
		a.MIME == b.MIME &&
		a.Detail == b.Detail &&
		a.Source == b.Source
}
