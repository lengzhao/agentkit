package session

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
)

// IndexedMessage is a model-visible message with its primary source event seq.
type IndexedMessage = compaction.IndexedMessage

// IndexMessagesForCompaction rebuilds the model-visible list used for compaction,
// including the latest compaction summary and retained tail when present.
func IndexMessagesForCompaction(ctx context.Context, events []agentkit.SessionEvent) []IndexedMessage {
	agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)

	prevCompactionIdx := -1
	var prevData compaction.EventData
	for i, ev := range events {
		if ev.Type != agentkit.EventCompaction || !eventForAgent(ev, agentID) {
			continue
		}
		var data compaction.EventData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		prevCompactionIdx = i
		prevData = data
	}

	out := make([]IndexedMessage, 0, len(events))
	if prevCompactionIdx >= 0 {
		out = append(out, IndexedMessage{
			Message:     prevData.Summary,
			Seq:         events[prevCompactionIdx].Seq,
			IsTurnStart: false,
		})
		for _, msg := range prevData.RetainedTail {
			out = append(out, IndexedMessage{Message: msg, Seq: prevData.FirstKeptSeq, IsTurnStart: msg.Role == "user"})
		}
		out = appendIndexedEventMessages(out, events[prevCompactionIdx+1:], agentID)
		return out
	}

	return appendIndexedEventMessages(out, events, agentID)
}

func appendIndexedEventMessages(out []IndexedMessage, events []agentkit.SessionEvent, agentID agentkit.AgentID) []IndexedMessage {
	var pendingSkillLoads []IndexedMessage
	flushSkillLoads := func() {
		if len(pendingSkillLoads) == 0 {
			return
		}
		out = append(out, pendingSkillLoads...)
		pendingSkillLoads = nil
	}
	for _, ev := range events {
		if !eventForAgent(ev, agentID) {
			continue
		}
		indexed := indexEventMessages(ev)
		for _, item := range indexed {
			if isSkillLoadMessage(item.Message) {
				pendingSkillLoads = append(pendingSkillLoads, item)
				continue
			}
			out = append(out, item)
			if item.Message.Role == "tool" {
				flushSkillLoads()
			}
		}
	}
	flushSkillLoads()
	return out
}

func isSkillLoadMessage(msg agentkit.ModelMessage) bool {
	if msg.Role != "user" || len(msg.Content) != 1 {
		return false
	}
	part := msg.Content[0]
	return (part.Type == "text" || part.Type == "") && strings.HasPrefix(part.Text, "<skill name=\"")
}

func indexEventMessages(ev agentkit.SessionEvent) []IndexedMessage {
	switch ev.Type {
	case agentkit.EventUserMessage, agentkit.EventAssistantMessage:
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			return nil
		}
		return []IndexedMessage{{
			Message:     msg,
			Seq:         ev.Seq,
			IsTurnStart: ev.Type == agentkit.EventUserMessage,
		}}
	case agentkit.EventToolResult:
		var result agentkit.ToolResult
		if err := json.Unmarshal(ev.Data, &result); err != nil {
			return nil
		}
		return []IndexedMessage{{
			Message:     toolResultMessage(result),
			Seq:         ev.Seq,
			IsTurnStart: false,
		}}
	case agentkit.EventSkillLoad:
		var load skillLoadEvent
		if err := json.Unmarshal(ev.Data, &load); err != nil {
			return nil
		}
		return []IndexedMessage{{
			Message:     skillLoadMessage(load),
			Seq:         ev.Seq,
			IsTurnStart: true,
		}}
	case agentkit.EventTurnContinue:
		var data TurnContinueData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			return nil
		}
		out := make([]IndexedMessage, 0, len(data.Messages))
		for _, msg := range data.Messages {
			out = append(out, IndexedMessage{
				Message:     msg,
				Seq:         ev.Seq,
				IsTurnStart: msg.Role == "user",
			})
		}
		return out
	default:
		return nil
	}
}
