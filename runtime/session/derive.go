package session

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/cap/skill"
)

func deriveMessages(ctx context.Context, events []agentkit.SessionEvent, maxToolBytes int) []agentkit.ModelMessage {
	agentID := AgentIDFromContext(ctx)

	compactAfterSeq, summary, retainedTail := latestCompactionView(events, agentID)

	var out []agentkit.ModelMessage
	if summary != nil {
		out = append(out, *summary)
		out = append(out, retainedTail...)
	}

	var pendingSkillLoads []agentkit.ModelMessage
	flushSkillLoads := func() {
		if len(pendingSkillLoads) == 0 {
			return
		}
		out = append(out, pendingSkillLoads...)
		pendingSkillLoads = nil
	}

	for _, ev := range events {
		if ev.Seq <= compactAfterSeq {
			continue
		}
		if !eventForAgent(ev, agentID) {
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
			flushSkillLoads()
		case agentkit.EventSkillLoad:
			var load skillLoadEvent
			if err := json.Unmarshal(ev.Data, &load); err != nil {
				continue
			}
			// Skill loads are recorded during tool execution, before the tool
			// result event. Defer them so assistant tool_calls are immediately
			// followed by tool messages, as providers require.
			pendingSkillLoads = append(pendingSkillLoads, skillLoadMessage(load))
		case agentkit.EventTurnContinue:
			var data TurnContinueData
			if err := json.Unmarshal(ev.Data, &data); err != nil {
				continue
			}
			out = append(out, data.Messages...)
		}
	}
	flushSkillLoads()

	out = answerOrphanToolCalls(out)

	if maxToolBytes > 0 {
		out = compaction.PruneToolResults(out, maxToolBytes)
	}
	return out
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

func latestCompactionView(events []agentkit.SessionEvent, agentID agentkit.AgentID) (compactAfterSeq agentkit.EventSeq, summary *agentkit.ModelMessage, retainedTail []agentkit.ModelMessage) {
	seq, data, ok := latestCompactionForAgent(events, agentID)
	if !ok {
		return 0, nil, nil
	}
	compactAfterSeq = seq
	s := data.Summary
	summary = &s
	if len(data.RetainedTail) > 0 {
		return compactAfterSeq, summary, append([]agentkit.ModelMessage(nil), data.RetainedTail...)
	}
	// Legacy compaction entries without retainedTail replay events after BeforeSeq.
	compactAfterSeq = data.BeforeSeq
	return compactAfterSeq, summary, nil
}

// eventForAgent limits replay to the agent running the current turn. A session
// file may hold multiple agents when chat-api switches agents mid-conversation
// or when legacy channel-scoped files are still on disk.
func eventForAgent(ev agentkit.SessionEvent, agentID agentkit.AgentID) bool {
	if agentID == "" || ev.AgentID == "" {
		return true
	}
	return ev.AgentID == agentID
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
	Name         string `json:"name"`
	Description  string `json:"description"`
	Body         string `json:"body"`
	ResourceBase string `json:"resourceBase,omitempty"`
}

func skillLoadMessage(load skillLoadEvent) agentkit.ModelMessage {
	return agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: skill.RenderLoaded(skill.Content{
			Name:        load.Name,
			Description: load.Description,
			Body:        load.Body,
			Path:        load.ResourceBase,
		})}},
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
	if err != nil {
		return err
	}
	TrimCompacted(s, agentID, data.MemoryCutoffSeq())
	return nil
}

func AppendSkillLoad(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, content skill.Content) error {
	raw, err := json.Marshal(skillLoadEvent{
		Name:         content.Name,
		Description:  content.Description,
		Body:         content.Body,
		ResourceBase: content.Path,
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

// LatestSeq returns the highest durable event sequence for the session.
func LatestSeq(ctx context.Context, s agentkit.Session) (agentkit.EventSeq, error) {
	if seq, ok := s.(interface{ LatestSeq() agentkit.EventSeq }); ok {
		return seq.LatestSeq(), nil
	}
	events, err := ReadAllEvents(ctx, s)
	if err != nil {
		return 0, err
	}
	return LatestEventSeq(events), nil
}
