package session

import (
	"strings"

	"github.com/lengzhao/agentkit"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

// DefaultMaxStoredTextBytes caps text persisted in session history.
const DefaultMaxStoredTextBytes = 8192

// SanitizeModelMessageForStorage removes bulky media payloads and truncates long text
// before writing user/assistant messages to durable session history.
func SanitizeModelMessageForStorage(msg agentkit.ModelMessage, maxTextBytes int) agentkit.ModelMessage {
	if maxTextBytes <= 0 {
		maxTextBytes = DefaultMaxStoredTextBytes
	}
	out := msg
	out.Content = sanitizeContentParts(msg.Content, maxTextBytes)
	if len(msg.ToolCalls) > 0 {
		out.ToolCalls = make([]agentkit.ToolCall, len(msg.ToolCalls))
		for i, call := range msg.ToolCalls {
			out.ToolCalls[i] = call
			if len(call.Input) > maxTextBytes {
				out.ToolCalls[i].Input = append(append([]byte(nil), call.Input[:maxTextBytes]...), []byte("\n...[truncated]")...)
			}
		}
	}
	if len(msg.ToolResults) > 0 {
		out.ToolResults = make([]agentkit.ToolResult, len(msg.ToolResults))
		for i, result := range msg.ToolResults {
			out.ToolResults[i] = rtcompaction.TruncateToolResult(result, maxTextBytes)
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
