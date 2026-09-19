package compaction

import (
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
	rtmedia "github.com/lengzhao/agentkit/runtime/media"
)

// BoundOversizedIndexedMessages bounds each indexed message's model-visible size
// for compaction output (retained tail): oversized text is truncated, and
// hydratable attachment parts in messages whose logical size exceeds maxChars
// are replaced with a text hint, so the post-compaction view cannot re-inflate
// during hydration. Durable session events keep the original content.
func BoundOversizedIndexedMessages(indexed []capscompaction.IndexedMessage, maxChars int) ([]agentkit.ModelMessage, bool) {
	msgs := make([]agentkit.ModelMessage, len(indexed))
	for i, item := range indexed {
		msgs[i] = item.Message
	}
	if maxChars <= 0 {
		return msgs, false
	}
	changed := false
	for i, item := range indexed {
		size := item.LogicalChars
		if size <= 0 {
			size = estimateMessageChars(item.Message)
		}
		if size <= maxChars {
			continue
		}
		if msg, ok := neutralizeAttachmentParts(msgs[i]); ok {
			msgs[i] = msg
			changed = true
		}
	}
	out, truncated := TruncateOversizedMessageTexts(msgs, maxChars)
	return out, changed || truncated
}

// neutralizeAttachmentParts replaces hydratable attachment parts (and inline
// data: payloads) with a text hint that keeps the source reference, so the
// model can re-read the file via tools if it still needs the content.
func neutralizeAttachmentParts(msg agentkit.ModelMessage) (agentkit.ModelMessage, bool) {
	if len(msg.Content) == 0 {
		return msg, false
	}
	changed := false
	parts := make([]agentkit.ContentPart, 0, len(msg.Content))
	for _, part := range msg.Content {
		if !isHydratablePart(part) {
			parts = append(parts, part)
			continue
		}
		changed = true
		parts = append(parts, agentkit.ContentPart{Type: "text", Text: attachmentOmittedHint(part)})
	}
	if !changed {
		return msg, false
	}
	msg.Content = parts
	return msg, true
}

func isHydratablePart(part agentkit.ContentPart) bool {
	switch part.Type {
	case rtmedia.ContentTypeAttachmentRef, "image", "image_url", "document", "file", "audio", "video":
		return true
	case "text", "":
		return false
	default:
		return strings.HasPrefix(strings.TrimSpace(part.URL), "data:")
	}
}

func attachmentOmittedHint(part agentkit.ContentPart) string {
	name := strings.TrimSpace(part.Source)
	if name == "" {
		if u := strings.TrimSpace(part.URL); u != "" && !strings.HasPrefix(u, "data:") {
			name = u
		}
	}
	if name == "" {
		name = part.Type
	}
	hint := "[attachment omitted from retained context: " + name
	if mime := strings.TrimSpace(part.MIME); mime != "" {
		hint += " mime=" + mime
	}
	return hint + "]"
}

// TruncateOversizedMessageTexts truncates text content of messages that alone
// exceed maxChars, keeping the head plus a marker. It is the last-resort bound
// for retained history so post-compaction context stays under budget; durable
// session events keep the full text, only the model-visible view is cut.
func TruncateOversizedMessageTexts(messages []agentkit.ModelMessage, maxChars int) ([]agentkit.ModelMessage, bool) {
	if maxChars <= 0 {
		return messages, false
	}
	changed := false
	out := make([]agentkit.ModelMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		total := 0
		for _, part := range msg.Content {
			if part.Type == "text" || part.Type == "" {
				total += len(part.Text)
			}
		}
		if total <= maxChars {
			continue
		}
		changed = true
		budget := maxChars
		parts := make([]agentkit.ContentPart, 0, len(msg.Content)+1)
		for _, part := range msg.Content {
			if part.Type != "text" && part.Type != "" {
				parts = append(parts, part)
				continue
			}
			if budget <= 0 {
				continue
			}
			if len(part.Text) > budget {
				parts = append(parts, agentkit.ContentPart{
					Type: "text",
					Text: part.Text[:budget] + fmt.Sprintf("\n…[message truncated: %d chars omitted]", total-budget),
				})
				budget = 0
				continue
			}
			budget -= len(part.Text)
			parts = append(parts, part)
		}
		out[i].Content = parts
	}
	return out, changed
}
