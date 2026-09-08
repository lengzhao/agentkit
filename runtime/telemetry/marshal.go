package telemetry

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lengzhao/agentkit"
)

// FormatMessage JSON-encodes a model message for trace input/output display.
func FormatMessage(msg agentkit.ModelMessage) string {
	if msg.Role == "" && len(msg.Content) == 0 && len(msg.ToolCalls) == 0 && len(msg.ToolResults) == 0 {
		return ""
	}
	entry := messageEntryForExport(msg, 0)
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
	if attachments := exportAttachmentParts(msg.Content, 0); len(attachments) > 0 {
		if raw, err := json.Marshal(attachments); err == nil {
			if b.Len() > 0 {
				b.WriteString(" ")
			}
			b.WriteString(string(raw))
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
func SummarizeMessages(messages []agentkit.ModelMessage, maxFieldBytes int, redact bool) string {
	raw := ExportMessages(messages)
	if maxFieldBytes > 0 {
		raw = FormatGenerationInputForExport(nil, messages, maxFieldBytes, false)
	}
	if redact {
		raw = RedactJSON(raw)
	}
	return raw
}

func messageEntryForExport(msg agentkit.ModelMessage, maxFieldBytes int) map[string]any {
	entry := map[string]any{}
	if msg.Role != "" {
		entry["role"] = msg.Role
	}
	if text := textFromParts(msg.Content); text != "" {
		entry["content"] = truncateFieldText(text, maxFieldBytes)
	}
	if attachments := exportAttachmentParts(msg.Content, maxFieldBytes); len(attachments) > 0 {
		entry["attachments"] = attachments
	}
	if len(msg.ToolCalls) > 0 {
		entry["toolCalls"] = exportToolCalls(msg.ToolCalls, maxFieldBytes)
	}
	if len(msg.ToolResults) > 0 {
		entry["toolResults"] = exportToolResults(msg.ToolResults, maxFieldBytes)
	}
	return entry
}

func exportToolResults(results []agentkit.ToolResult, maxFieldBytes int) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, result := range results {
		entry := map[string]any{
			"id":   string(result.ID),
			"name": result.Name,
		}
		if result.Content != "" {
			entry["content"] = truncateFieldText(result.Content, maxFieldBytes)
		}
		out = append(out, entry)
	}
	return out
}

func exportToolCalls(calls []agentkit.ToolCall, maxFieldBytes int) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		out = append(out, map[string]any{
			"id":    string(call.ID),
			"name":  call.Name,
			"input": truncateFieldText(string(call.Input), maxFieldBytes),
		})
	}
	return out
}

const fieldTruncationSuffix = "\n...[truncated]"

func truncateFieldText(text string, maxBytes int) string {
	if maxBytes <= 0 || text == "" || len(text) <= maxBytes {
		return text
	}
	suffix := fieldTruncationSuffix
	limit := maxBytes - len(suffix)
	if limit < 1 {
		return TruncatePayload(text, maxBytes)
	}
	cut := text[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + suffix
}

func exportAttachmentParts(parts []agentkit.ContentPart, maxFieldBytes int) []map[string]any {
	var out []map[string]any
	for _, part := range parts {
		if isTextOnlyPart(part) {
			continue
		}
		if exported := exportContentPart(part, maxFieldBytes); exported != nil {
			out = append(out, exported)
		}
	}
	return out
}

func exportContentPart(part agentkit.ContentPart, maxFieldBytes int) map[string]any {
	typ := strings.TrimSpace(part.Type)
	if typ == "thinking" {
		return nil
	}
	if typ == "" {
		if part.Source != "" || part.URL != "" {
			typ = "attachment_ref"
		} else {
			return nil
		}
	}
	entry := map[string]any{"type": typ}
	if text := strings.TrimSpace(part.Text); text != "" && !strings.HasPrefix(text, "data:") {
		entry["text"] = truncateFieldText(text, maxFieldBytes)
	}
	if mime := strings.TrimSpace(part.MIME); mime != "" {
		entry["mime"] = mime
	}
	if source := strings.TrimSpace(part.Source); source != "" {
		entry["source"] = source
	}
	if detail := strings.TrimSpace(part.Detail); detail != "" {
		entry["detail"] = detail
	}
	if url := strings.TrimSpace(part.URL); url != "" {
		entry["url"] = summarizeDataURL(url)
	}
	if len(entry) == 1 {
		return nil
	}
	return entry
}

func isTextOnlyPart(part agentkit.ContentPart) bool {
	typ := strings.TrimSpace(part.Type)
	switch typ {
	case "", "text":
		return part.Source == "" && part.URL == "" && part.MIME == "" && part.Detail == ""
	default:
		return false
	}
}

func summarizeDataURL(url string) string {
	url = strings.TrimSpace(url)
	if !strings.HasPrefix(url, "data:") {
		return url
	}
	semi := strings.Index(url, ";")
	if semi < 0 {
		return "[data URL]"
	}
	mime := url[5:semi]
	payload := url[semi+1:]
	if strings.HasPrefix(payload, "base64,") {
		approxBytes := (len(payload) - len("base64,")) * 3 / 4
		return fmt.Sprintf("[data:%s;base64,%d bytes]", mime, approxBytes)
	}
	return fmt.Sprintf("[data:%s]", mime)
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
