package compaction

import (
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

// summaryMaxMessageChars caps one serialized message in summarization input, so
// a single giant message (skill injection, pasted log) cannot blow the summary
// model's window.
const summaryMaxMessageChars = 100_000

// SerializeConversation formats messages as plain text for summarization (Pi-style).
func SerializeConversation(messages []agentkit.ModelMessage) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(serializeMessageBlock(msg))
		b.WriteByte('\n')
	}
	return b.String()
}

// SerializeConversationWithBudget is SerializeConversation bounded for the
// summary model's window: each message is capped at summaryMaxMessageChars and
// when the total exceeds maxTotalChars the oldest messages are dropped with an
// omission marker (recent context matters more for continuation). A
// maxTotalChars <= 0 disables the total budget.
func SerializeConversationWithBudget(messages []agentkit.ModelMessage, maxTotalChars int) string {
	blocks := make([]string, 0, len(messages))
	for _, msg := range messages {
		block := serializeMessageBlock(msg)
		if len(block) > summaryMaxMessageChars {
			block = block[:summaryMaxMessageChars] +
				fmt.Sprintf("\n…[message truncated: %d chars omitted]", len(block)-summaryMaxMessageChars)
		}
		blocks = append(blocks, block)
	}
	if maxTotalChars > 0 {
		total := 0
		for _, block := range blocks {
			total += len(block) + 1
		}
		dropped := 0
		for len(blocks) > 1 && total > maxTotalChars {
			total -= len(blocks[0]) + 1
			blocks = blocks[1:]
			dropped++
		}
		if dropped > 0 {
			blocks = append([]string{fmt.Sprintf("[... %d oldest messages omitted to fit the summarization budget ...]", dropped)}, blocks...)
		}
	}
	var b strings.Builder
	for _, block := range blocks {
		b.WriteString(block)
		b.WriteByte('\n')
	}
	return b.String()
}

func serializeMessageBlock(msg agentkit.ModelMessage) string {
	var b strings.Builder
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
		b.WriteString("[")
		b.WriteString(msg.Role)
		b.WriteString("]: ")
		b.WriteString(messageText(msg))
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
