package session

import (
	"encoding/json"

	"github.com/lengzhao/agentkit"
)

// indexableMessage is one searchable row from a session log.
type indexableMessage struct {
	Seq  int64
	Role string
	Text string
}

// ExtractIndexableMessages returns user/assistant text from session events.
func ExtractIndexableMessages(events []agentkit.SessionEvent) []indexableMessage {
	var out []indexableMessage
	for _, ev := range events {
		switch ev.Type {
		case agentkit.EventUserMessage, agentkit.EventAssistantMessage:
			msg, err := unmarshalModelMessage(ev.Data)
			if err != nil {
				continue
			}
			text := FlattenTextParts(msg.Content, "\n")
			if text == "" {
				continue
			}
			out = append(out, indexableMessage{
				Seq:  int64(ev.Seq),
				Role: msg.Role,
				Text: text,
			})
		}
	}
	return out
}

func unmarshalModelMessage(raw json.RawMessage) (agentkit.ModelMessage, error) {
	var msg agentkit.ModelMessage
	if len(raw) == 0 {
		return msg, nil
	}
	err := json.Unmarshal(raw, &msg)
	return msg, err
}

