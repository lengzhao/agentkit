package session

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
)

// IndexedMessage is a model-visible message with its primary source event seq.
type IndexedMessage = compaction.IndexedMessage

// IndexMessagesForCompaction rebuilds the model-visible list used for compaction,
// including the latest compaction summary and retained tail when present.
func IndexMessagesForCompaction(ctx context.Context, events []agentkit.SessionEvent, userMessageTemplate string) []IndexedMessage {
	agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)
	tmpl := resolveUserMessageTemplate(ctx, userMessageTemplate)

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
		for _, ev := range events[prevCompactionIdx+1:] {
			out = append(out, indexEventMessages(ctx, ev, tmpl)...)
		}
		return out
	}

	for _, ev := range events {
		if !eventForAgent(ev, agentID) {
			continue
		}
		out = append(out, indexEventMessages(ctx, ev, tmpl)...)
	}
	return out
}

func indexEventMessages(ctx context.Context, ev agentkit.SessionEvent, tmpl string) []IndexedMessage {
	switch ev.Type {
	case agentkit.EventUserMessage, agentkit.EventAssistantMessage:
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			return nil
		}
		if ev.Type == agentkit.EventUserMessage {
			msg = applyUserMessageTemplate(msg, ev.UserID, ev.Metadata, tmpl)
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

func latestCompactionForAgent(events []agentkit.SessionEvent, agentID agentkit.AgentID) (agentkit.EventSeq, compaction.EventData, bool) {
	var (
		seq  agentkit.EventSeq
		data compaction.EventData
		ok   bool
	)
	for _, ev := range events {
		if ev.Type != agentkit.EventCompaction || !eventForAgent(ev, agentID) {
			continue
		}
		var parsed compaction.EventData
		if err := json.Unmarshal(ev.Data, &parsed); err != nil {
			continue
		}
		seq = ev.Seq
		data = parsed
		ok = true
	}
	return seq, data, ok
}
