package derive

import (
	"strings"

	"github.com/lengzhao/agentkit"
)

// HasUserInstruction reports whether projected history contains user intent,
// excluding generated attachment-only prompts. Legacy hint formats remain here
// so transport adapters need not understand model-visible presentation text.
func HasUserInstruction(messages []agentkit.ModelMessage) bool {
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, part := range msg.Content {
			if part.Type != "text" {
				continue
			}
			text := strings.TrimSpace(part.Text)
			text = strings.TrimSpace(strings.SplitN(text, "\n\n(Files the user uploaded with this message. workspace paths:", 2)[0])
			if text != "" && text != "Please analyze the attached file(s)." && !strings.HasPrefix(text, "[附件") && !strings.HasPrefix(text, "[attachment") {
				return true
			}
		}
	}
	return false
}
