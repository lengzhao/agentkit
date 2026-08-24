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
		case agentkit.EventTurnContinue:
			var data TurnContinueData
			if err := json.Unmarshal(ev.Data, &data); err != nil {
				continue
			}
			out = append(out, data.Messages...)
		}
	}

	out = answerOrphanToolCalls(out)

	if maxToolBytes > 0 {
		out = compaction.PruneToolResults(out, maxToolBytes)
	}
	return out
}

// answerOrphanToolCalls inserts a stand-in result for every tool call the
// history never answers. Providers reject an assistant message whose tool calls
// have no replies, so without this a session interrupted mid-tool could never be
// replayed — not even to summarize or repair it.
func answerOrphanToolCalls(messages []agentkit.ModelMessage) []agentkit.ModelMessage {
	// Result positions per call ID, consumed in order: an ID may legitimately
	// repeat across steps, and only a later result answers a given call.
	positions := make(map[agentkit.ToolCallID][]int)
	for i, msg := range messages {
		for _, result := range msg.ToolResults {
			positions[result.ID] = append(positions[result.ID], i)
		}
	}

	var orphansAt map[int][]agentkit.ToolResult
	for i, msg := range messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}
		for _, call := range msg.ToolCalls {
			if consumeResultAfter(positions, call.ID, i) {
				continue
			}
			if orphansAt == nil {
				orphansAt = make(map[int][]agentkit.ToolResult)
			}
			orphansAt[i] = append(orphansAt[i], InterruptedToolResult(call))
		}
	}
	if len(orphansAt) == 0 {
		return messages
	}

	out := make([]agentkit.ModelMessage, 0, len(messages)+len(orphansAt))
	for i, msg := range messages {
		out = append(out, msg)
		if results, ok := orphansAt[i]; ok {
			out = append(out, agentkit.ModelMessage{Role: "tool", ToolResults: results})
		}
	}
	return out
}

// consumeResultAfter claims the earliest unconsumed result for id that sits
// after index, reporting whether the call is answered.
func consumeResultAfter(positions map[agentkit.ToolCallID][]int, id agentkit.ToolCallID, index int) bool {
	indexes := positions[id]
	for i, pos := range indexes {
		if pos <= index {
			continue
		}
		positions[id] = append(indexes[:i:i], indexes[i+1:]...)
		return true
	}
	return false
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
