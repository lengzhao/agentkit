package compaction

import (
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

// SerializeConversation formats messages as plain text for summarization (Pi-style).
func SerializeConversation(messages []agentkit.ModelMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			b.WriteString("[User]: ")
			b.WriteString(messageText(msg))
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				b.WriteString("[Assistant tool calls]: ")
				for i, call := range msg.ToolCalls {
					if i > 0 {
						b.WriteString("; ")
					}
					fmt.Fprintf(&b, "%s(%s)", call.Name, string(call.Input))
				}
			} else {
				b.WriteString("[Assistant]: ")
				b.WriteString(messageText(msg))
			}
		case "tool":
			b.WriteString("[Tool result]: ")
			for i, result := range msg.ToolResults {
				if i > 0 {
					b.WriteString("; ")
				}
				b.WriteString(result.Content)
			}
		default:
			b.WriteString("[" + msg.Role + "]: ")
			b.WriteString(messageText(msg))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func messageText(msg agentkit.ModelMessage) string {
	var parts []string
	for _, part := range msg.Content {
		if part.Type == "text" && part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}
