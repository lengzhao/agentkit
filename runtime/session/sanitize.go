package session

import (
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

// DefaultMaxStoredTextBytes caps embedded tool-result text and spill preview views (pi-style
// truncation applies at tool execution / PrepareToolResultForStorage, not chat message bodies).
const DefaultMaxStoredTextBytes = 8192

// SanitizeModelMessageForStorage strips bulky inline media before durable session history.
// User and assistant message text is persisted in full (aligned with pi session JSONL).
// When maxTextBytes > 0, only tests or explicit callers may cap message body text.
func SanitizeModelMessageForStorage(msg agentkit.ModelMessage, maxTextBytes int) agentkit.ModelMessage {
	contentMaxBytes := maxTextBytes
	if contentMaxBytes < 0 {
		contentMaxBytes = 0
	}
	toolResultMax := maxTextBytes
	if toolResultMax <= 0 {
		toolResultMax = DefaultMaxStoredTextBytes
	}
	out := msg
	out.Content = sanitizeContentParts(msg.Content, contentMaxBytes)
	if len(msg.ToolCalls) > 0 {
		out.ToolCalls = make([]agentkit.ToolCall, len(msg.ToolCalls))
		for i, call := range msg.ToolCalls {
			out.ToolCalls[i] = call
			out.ToolCalls[i].Input = sanitizeToolCallInputForStorage(call.Input)
		}
	}
	if len(msg.ToolResults) > 0 {
		out.ToolResults = make([]agentkit.ToolResult, len(msg.ToolResults))
		for i, result := range msg.ToolResults {
			out.ToolResults[i] = TruncateToolResult(result, toolResultMax)
		}
	}
	return out
}

func sanitizeContentParts(parts []agentkit.ContentPart, maxTextBytes int) []agentkit.ContentPart {
	if len(parts) == 0 {
		return parts
	}
	out := make([]agentkit.ContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "thinking":
			continue
		case rtmedia.ContentTypeAttachmentRef:
			out = append(out, part)
		case "image", "image_url", "document", "file", "audio", "video":
			if ref := sanitizeAttachmentRef(part); ref != nil {
				out = append(out, *ref)
			}
		case "text", "":
			text := strings.TrimSpace(part.Text)
			if isDataURL(text) {
				continue
			}
			if text == "" {
				continue
			}
			out = append(out, agentkit.ContentPart{Type: "text", Text: truncateText(text, maxTextBytes)})
		default:
			if isDataURL(part.URL) || isDataURL(part.Text) {
				continue
			}
			if isAttachmentType(part.Type) || part.URL != "" {
				if ref := sanitizeAttachmentRef(part); ref != nil {
					out = append(out, *ref)
				}
				continue
			}
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			out = append(out, agentkit.ContentPart{Type: "text", Text: truncateText(text, maxTextBytes)})
		}
	}
	return out
}

func sanitizeAttachmentRef(part agentkit.ContentPart) *agentkit.ContentPart {
	ref := agentkit.ContentPart{
		Type: rtmedia.ContentTypeAttachmentRef,
		MIME: strings.TrimSpace(part.MIME),
	}
	if src := strings.TrimSpace(part.Source); src != "" {
		ref.Source = src
		return &ref
	}
	if url := strings.TrimSpace(part.URL); url != "" && !isDataURL(url) {
		ref.URL = url
		return &ref
	}
	return nil
}

func isAttachmentType(typ string) bool {
	switch typ {
	case "image", "image_url", "document", "file", "audio", "video":
		return true
	default:
		return false
	}
}

func isDataURL(raw string) bool {
	return strings.HasPrefix(strings.TrimSpace(raw), "data:")
}

func truncateText(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	return text[:maxBytes] + "\n...[truncated]"
}

// sanitizeToolCallInputForStorage keeps ToolCall.Input valid JSON so json.Marshal on
// ModelMessage and ToolCall never fails (json.RawMessage must be valid JSON).
// Valid arguments are persisted in full (aligned with pi session history); only
// empty or malformed payloads are normalized.
func sanitizeToolCallInputForStorage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	if json.Valid(raw) {
		return raw
	}
	return toolCallInputInvalidJSONPlaceholder(raw)
}

func toolCallInputInvalidJSONPlaceholder(raw json.RawMessage) json.RawMessage {
	preview := string(raw)
	if len(preview) > DefaultMaxStoredTextBytes {
		preview = preview[:DefaultMaxStoredTextBytes] + "\n...[truncated]"
	}
	placeholder := map[string]string{
		"_storage_note": "tool call arguments were not valid JSON",
		"preview":       preview,
	}
	out, err := json.Marshal(placeholder)
	if err != nil {
		return json.RawMessage(`{"_storage_note":"tool call arguments omitted"}`)
	}
	return out
}

// SanitizeToolCall prepares a tool call for durable session storage.
func SanitizeToolCall(call agentkit.ToolCall) agentkit.ToolCall {
	call.Input = sanitizeToolCallInputForStorage(call.Input)
	return call
}
