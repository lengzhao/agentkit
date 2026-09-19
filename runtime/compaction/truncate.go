package compaction

import (
	"fmt"

	"github.com/lengzhao/agentkit"
)

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
