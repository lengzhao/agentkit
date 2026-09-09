package session

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/cap/skill"
)

// IndexedMessage is a model-visible message with its primary source event seq.
type IndexedMessage = compaction.IndexedMessage

// IndexMessagesForCompaction rebuilds the model-visible list used for compaction,
// including the latest compaction summary and retained tail when present.
func IndexMessagesForCompaction(ctx context.Context, events []agentkit.SessionEvent) []IndexedMessage {
	agentID := AgentIDFromContext(ctx)
	view := resolveCompactionView(events, agentID)
	out := indexedCompactionPrefix(view)
	out = append(out, walkIndexedEvents(events, agentID, view.AfterSeq)...)
	return out
}

func deriveMessages(ctx context.Context, events []agentkit.SessionEvent, maxToolBytes int) []agentkit.ModelMessage {
	agentID := AgentIDFromContext(ctx)
	view := resolveCompactionView(events, agentID)
	out := plainCompactionPrefix(view)
	out = append(out, walkPlainEvents(events, agentID, view.AfterSeq)...)
	out = answerOrphanToolCalls(out)
	if maxToolBytes > 0 {
		out = PruneToolResults(out, maxToolBytes)
	}
	return out
}

type compactionView struct {
	AfterSeq      agentkit.EventSeq
	Summary       *agentkit.ModelMessage
	RetainedTail  []agentkit.ModelMessage
	CompactionSeq agentkit.EventSeq
	FirstKeptSeq  agentkit.EventSeq
	HasCompaction bool
}

func resolveCompactionView(events []agentkit.SessionEvent, agentID agentkit.AgentID) compactionView {
	seq, data, ok := latestCompactionForAgent(events, agentID)
	if !ok {
		return compactionView{}
	}
	summary := data.Summary
	view := compactionView{
		HasCompaction: true,
		CompactionSeq: seq,
		Summary:       &summary,
		FirstKeptSeq:  data.FirstKeptSeq,
		AfterSeq:      seq,
	}
	if len(data.RetainedTail) > 0 {
		view.RetainedTail = append([]agentkit.ModelMessage(nil), data.RetainedTail...)
		return view
	}
	// Legacy compaction entries without retainedTail replay events after BeforeSeq.
	view.AfterSeq = data.BeforeSeq
	return view
}

func plainCompactionPrefix(view compactionView) []agentkit.ModelMessage {
	if !view.HasCompaction || view.Summary == nil {
		return nil
	}
	out := []agentkit.ModelMessage{*view.Summary}
	return append(out, view.RetainedTail...)
}

func indexedCompactionPrefix(view compactionView) []IndexedMessage {
	if !view.HasCompaction || view.Summary == nil {
		return nil
	}
	out := []IndexedMessage{{
		Message:     *view.Summary,
		Seq:         view.CompactionSeq,
		IsTurnStart: false,
	}}
	for _, msg := range view.RetainedTail {
		out = append(out, IndexedMessage{
			Message:     msg,
			Seq:         view.FirstKeptSeq,
			IsTurnStart: msg.Role == "user",
		})
	}
	return out
}

type visibleWalkItem struct {
	msg         agentkit.ModelMessage
	seq         agentkit.EventSeq
	isTurnStart bool
	deferSkill  bool
}

func walkPlainEvents(events []agentkit.SessionEvent, agentID agentkit.AgentID, afterSeq agentkit.EventSeq) []agentkit.ModelMessage {
	items := collectVisibleEvents(events, agentID, afterSeq)
	out := make([]agentkit.ModelMessage, 0, len(items))
	for _, item := range items {
		out = append(out, item.msg)
	}
	return out
}

func walkIndexedEvents(events []agentkit.SessionEvent, agentID agentkit.AgentID, afterSeq agentkit.EventSeq) []IndexedMessage {
	items := collectVisibleEvents(events, agentID, afterSeq)
	out := make([]IndexedMessage, 0, len(items))
	for _, item := range items {
		out = append(out, IndexedMessage{
			Message:     item.msg,
			Seq:         item.seq,
			IsTurnStart: item.isTurnStart,
		})
	}
	return out
}

func collectVisibleEvents(events []agentkit.SessionEvent, agentID agentkit.AgentID, afterSeq agentkit.EventSeq) []visibleWalkItem {
	var out []visibleWalkItem
	var pendingSkill []visibleWalkItem
	flushSkillLoads := func() {
		if len(pendingSkill) == 0 {
			return
		}
		out = append(out, pendingSkill...)
		pendingSkill = nil
	}
	for _, ev := range events {
		if ev.Seq <= afterSeq {
			continue
		}
		if !eventForAgent(ev, agentID) {
			continue
		}
		for _, item := range eventToWalkItems(ev) {
			if item.deferSkill {
				pendingSkill = append(pendingSkill, item)
				continue
			}
			out = append(out, item)
			if item.msg.Role == "tool" {
				flushSkillLoads()
			}
		}
	}
	flushSkillLoads()
	return out
}

func eventToWalkItems(ev agentkit.SessionEvent) []visibleWalkItem {
	switch ev.Type {
	case agentkit.EventUserMessage, agentkit.EventAssistantMessage:
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			return nil
		}
		return []visibleWalkItem{{
			msg:         msg,
			seq:         ev.Seq,
			isTurnStart: ev.Type == agentkit.EventUserMessage,
		}}
	case agentkit.EventToolResult:
		var result agentkit.ToolResult
		if err := json.Unmarshal(ev.Data, &result); err != nil {
			return nil
		}
		return []visibleWalkItem{{
			msg:         toolResultMessage(result),
			seq:         ev.Seq,
			isTurnStart: false,
		}}
	case agentkit.EventSkillLoad:
		var load skillLoadEvent
		if err := json.Unmarshal(ev.Data, &load); err != nil {
			return nil
		}
		// Skill loads are recorded during tool execution, before the tool
		// result event. Defer them so assistant tool_calls are immediately
		// followed by tool messages, as providers require.
		return []visibleWalkItem{{
			msg:         skillLoadMessage(load),
			seq:         ev.Seq,
			isTurnStart: true,
			deferSkill:  true,
		}}
	case agentkit.EventTurnContinue:
		var data TurnContinueData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			return nil
		}
		out := make([]visibleWalkItem, 0, len(data.Messages))
		for _, msg := range data.Messages {
			out = append(out, visibleWalkItem{
				msg:         msg,
				seq:         ev.Seq,
				isTurnStart: msg.Role == "user",
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
		Rendered:     renderSkillLoaded(content),
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
