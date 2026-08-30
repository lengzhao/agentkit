package telemetry

import (
	"encoding/json"

	"github.com/lengzhao/agentkit"
)

// FormatMessage JSON-encodes a model message for trace input/output display.
func FormatMessage(msg agentkit.ModelMessage) string {
	if msg.Role == "" && len(msg.Content) == 0 && len(msg.ToolCalls) == 0 && len(msg.ToolResults) == 0 {
		return ""
	}
	entry := map[string]any{}
	if msg.Role != "" {
		entry["role"] = msg.Role
	}
	if text := textFromParts(msg.Content); text != "" {
		entry["content"] = text
	}
	if len(msg.ToolCalls) > 0 {
		entry["toolCalls"] = msg.ToolCalls
	}
	if len(msg.ToolResults) > 0 {
		entry["toolResults"] = msg.ToolResults
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return SummarizeMessage(msg)
	}
	return string(raw)
}

// ToolNamesFromSpecs returns tool names exposed to the model for one step.
func ToolNamesFromSpecs(specs []agentkit.ToolSpec) []string {
	if len(specs) == 0 {
		return nil
	}
	names := make([]string, len(specs))
	for i, spec := range specs {
		names[i] = spec.Name
	}
	return names
}

// SummarizeMessage returns a compact text summary of a model message.
func SummarizeMessage(msg agentkit.ModelMessage) string {
	if msg.Role == "" && len(msg.Content) == 0 && len(msg.ToolCalls) == 0 {
		return ""
	}
	var b stringsBuilder
	if msg.Role != "" {
		b.WriteString(msg.Role)
		b.WriteString(": ")
	}
	for _, part := range msg.Content {
		if part.Text != "" {
			b.WriteString(part.Text)
		}
	}
	if len(msg.ToolCalls) > 0 {
		calls := make([]map[string]string, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			calls = append(calls, map[string]string{
				"id":    string(call.ID),
				"name":  call.Name,
				"input": string(call.Input),
			})
		}
		if raw, err := json.Marshal(calls); err == nil {
			if b.Len() > 0 {
				b.WriteString(" ")
			}
			b.WriteString(string(raw))
		}
	}
	return b.String()
}

// SummarizeMessages JSON-encodes a message list for exporter input.
func SummarizeMessages(messages []agentkit.ModelMessage, maxBytes int, redact bool) string {
	if len(messages) == 0 {
		return ""
	}
	summaries := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		entry := map[string]any{"role": msg.Role}
		if text := textFromParts(msg.Content); text != "" {
			entry["content"] = text
		}
		if len(msg.ToolCalls) > 0 {
			entry["toolCalls"] = msg.ToolCalls
		}
		summaries = append(summaries, entry)
	}
	raw, err := json.Marshal(summaries)
	if err != nil {
		return ""
	}
	return PreparePayload(string(raw), maxBytes, redact)
}

func textFromParts(parts []agentkit.ContentPart) string {
	var b stringsBuilder
	for _, part := range parts {
		if part.Text != "" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

type stringsBuilder struct {
	buf []byte
}

func (b *stringsBuilder) WriteString(s string) {
	b.buf = append(b.buf, s...)
}

func (b *stringsBuilder) Len() int { return len(b.buf) }

func (b *stringsBuilder) String() string { return string(b.buf) }
