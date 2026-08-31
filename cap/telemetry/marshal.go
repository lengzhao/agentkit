package telemetry

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/media"
)

// FormatMessage JSON-encodes a model message for trace input/output display.
func FormatMessage(msg agentkit.ModelMessage) string {
	if msg.Role == "" && len(msg.Content) == 0 && len(msg.ToolCalls) == 0 && len(msg.ToolResults) == 0 {
		return ""
	}
	entry := messageEntryForExport(msg)
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
	if attachments := exportAttachmentParts(msg.Content); len(attachments) > 0 {
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
func SummarizeMessages(messages []agentkit.ModelMessage, maxBytes int, redact bool) string {
	if len(messages) == 0 {
		return ""
	}
	summaries := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		entry := messageEntryForExport(msg)
		summaries = append(summaries, entry)
	}
	raw, err := json.Marshal(summaries)
	if err != nil {
		return ""
	}
	return PreparePayload(string(raw), maxBytes, redact)
}

func messageEntryForExport(msg agentkit.ModelMessage) map[string]any {
	entry := map[string]any{}
	if msg.Role != "" {
		entry["role"] = msg.Role
	}
	if text := textFromParts(msg.Content); text != "" {
		entry["content"] = text
	}
	if attachments := exportAttachmentParts(msg.Content); len(attachments) > 0 {
		entry["attachments"] = attachments
	}
	if len(msg.ToolCalls) > 0 {
		entry["toolCalls"] = msg.ToolCalls
	}
	return entry
}

func exportAttachmentParts(parts []agentkit.ContentPart) []map[string]any {
	var out []map[string]any
	for _, part := range parts {
		if isTextOnlyPart(part) {
			continue
		}
		if exported := exportContentPart(part); exported != nil {
			out = append(out, exported)
		}
	}
	return out
}

func exportContentPart(part agentkit.ContentPart) map[string]any {
	typ := strings.TrimSpace(part.Type)
	if typ == "thinking" {
		return nil
	}
	if typ == "" {
		if part.Source != "" || part.URL != "" {
			typ = media.ContentTypeAttachmentRef
		} else {
			return nil
		}
	}
	entry := map[string]any{"type": typ}
	if text := strings.TrimSpace(part.Text); text != "" && !strings.HasPrefix(text, "data:") {
		entry["text"] = text
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
