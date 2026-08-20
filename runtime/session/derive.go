package session

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
)

func deriveMessages(events []agentkit.SessionEvent, maxToolBytes int) []agentkit.ModelMessage {
	var compactBefore agentkit.EventSeq
	var summary *agentkit.ModelMessage

	for _, ev := range events {
		if ev.Type != agentkit.EventCompaction {
			continue
		}
		var data compaction.EventData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		compactBefore = data.BeforeSeq
		s := data.Summary
		summary = &s
	}

	var out []agentkit.ModelMessage
	if summary != nil {
		out = append(out, *summary)
	}

	for _, ev := range events {
		if ev.Seq <= compactBefore {
			continue
		}
		switch ev.Type {
		case agentkit.EventUserMessage, agentkit.EventAssistantMessage:
			var msg agentkit.ModelMessage
			if err := json.Unmarshal(ev.Data, &msg); err != nil {
				continue
			}
			out = append(out, msg)
		case agentkit.EventToolResult:
			var result agentkit.ToolResult
			if err := json.Unmarshal(ev.Data, &result); err != nil {
				continue
			}
			out = append(out, toolResultMessage(result))
		case agentkit.EventSkillLoad:
			var load skillLoadEvent
			if err := json.Unmarshal(ev.Data, &load); err != nil {
				continue
			}
			out = append(out, skillLoadMessage(load))
		}
	}

	if maxToolBytes > 0 {
		out = compaction.PruneToolResults(out, maxToolBytes)
	}
	return out
}

type skillLoadEvent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

func skillLoadMessage(load skillLoadEvent) agentkit.ModelMessage {
	text := "<skill name=\"" + load.Name + "\">\n" + load.Body + "\n</skill>"
	return agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: text}},
	}
}

func AppendCompaction(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data compaction.EventData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.Append(ctx, agentkit.SessionEvent{
		AgentID: agentID,
		Type:    agentkit.EventCompaction,
		Data:    raw,
	})
	return err
}

func AppendSkillLoad(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, name, description, body string) error {
	raw, err := json.Marshal(skillLoadEvent{
		Name:        name,
		Description: description,
		Body:        body,
	})
	if err != nil {
		return err
	}
	_, err = s.Append(ctx, agentkit.SessionEvent{
		AgentID: agentID,
		Type:    agentkit.EventSkillLoad,
		Data:    raw,
	})
	return err
}

func LatestEventSeq(events []agentkit.SessionEvent) agentkit.EventSeq {
	var seq agentkit.EventSeq
	for _, ev := range events {
		if ev.Seq > seq {
			seq = ev.Seq
		}
	}
	return seq
}

func ReadAllEvents(ctx context.Context, s agentkit.Session) ([]agentkit.SessionEvent, error) {
	return s.Read(ctx, 0)
}
