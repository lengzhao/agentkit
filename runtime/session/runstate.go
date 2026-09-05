package session

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
	capsess "github.com/lengzhao/agentkit/cap/session"
)

type (
	Todo             = capsess.Todo
	TodoUpdateData   = capsess.TodoUpdateData
	RunFinishData    = capsess.RunFinishData
	TurnContinueData = capsess.TurnContinueData
	UsageData        = capsess.UsageData
	RunState         = capsess.RunState
)

const (
	TodoPending     = capsess.TodoPending
	TodoInProgress  = capsess.TodoInProgress
	TodoDone        = capsess.TodoDone
	FinishCompleted = capsess.FinishCompleted
	FinishBlocked   = capsess.FinishBlocked
)

// LoadRunState reads autonomous-run signals for the given session.
func LoadRunState(ctx context.Context, store agentkit.SessionStore, sessionID agentkit.SessionID) (RunState, error) {
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		return RunState{}, err
	}
	events, err := ReadAllEvents(ctx, sess)
	if err != nil {
		return RunState{}, err
	}
	startSeq := RunStartSeq(events)
	todos := LatestTodos(events)
	return RunState{
		StartSeq: startSeq,
		Todos:    todos,
		Pending:  PendingTodos(todos),
		Finish:   FinishAfter(events, startSeq),
		Repeats:  RepeatedToolCalls(events, startSeq),
		Usage:    TotalUsage(events, startSeq),
		Context:  LatestUsage(events).InputTokens,
	}, nil
}

func AppendTodoUpdate(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, items []Todo) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventTodoUpdate, TodoUpdateData{Items: items})
}

func AppendRunFinish(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data RunFinishData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventRunFinish, data)
}

func AppendTurnContinue(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data TurnContinueData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventTurnContinue, data)
}

func AppendUsage(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data UsageData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventUsage, data)
}

// LatestTodos returns the task list from the most recent todo/update event.
func LatestTodos(events []agentkit.SessionEvent) []Todo {
	var items []Todo
	for _, ev := range events {
		if ev.Type != agentkit.EventTodoUpdate {
			continue
		}
		var data TodoUpdateData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		items = data.Items
	}
	return items
}

// PendingTodos filters LatestTodos down to entries that still need work.
func PendingTodos(items []Todo) []Todo {
	var out []Todo
	for _, item := range items {
		if !item.Done() {
			out = append(out, item)
		}
	}
	return out
}

// FinishAfter returns the run/finish event recorded after seq, or nil.
func FinishAfter(events []agentkit.SessionEvent, seq agentkit.EventSeq) *RunFinishData {
	var out *RunFinishData
	for _, ev := range events {
		if ev.Type != agentkit.EventRunFinish || ev.Seq <= seq {
			continue
		}
		var data RunFinishData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		copied := data
		out = &copied
	}
	return out
}

// LastAssistantText returns the text of the most recent assistant message after seq.
func LastAssistantText(events []agentkit.SessionEvent, seq agentkit.EventSeq) string {
	var out string
	for _, ev := range events {
		if ev.Type != agentkit.EventAssistantMessage || ev.Seq <= seq {
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			continue
		}
		var b strings.Builder
		for _, part := range msg.Content {
			if part.Type == "text" {
				b.WriteString(part.Text)
			}
		}
		if text := strings.TrimSpace(b.String()); text != "" {
			out = text
		}
	}
	return out
}

// StepCount counts the steps completed after seq.
func StepCount(events []agentkit.SessionEvent, seq agentkit.EventSeq) int {
	count := 0
	for _, ev := range events {
		if ev.Type == agentkit.EventStepEnd && ev.Seq > seq {
			count++
		}
	}
	return count
}

// RunStartSeq returns the seq of the most recent inbound user message.
func RunStartSeq(events []agentkit.SessionEvent) agentkit.EventSeq {
	var seq agentkit.EventSeq
	for _, ev := range events {
		if ev.Type == agentkit.EventUserMessage {
			seq = ev.Seq
		}
	}
	return seq
}

// TotalUsage sums usage events recorded after seq.
func TotalUsage(events []agentkit.SessionEvent, seq agentkit.EventSeq) UsageData {
	var out UsageData
	for _, ev := range events {
		if ev.Type != agentkit.EventUsage || ev.Seq <= seq {
			continue
		}
		var data UsageData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		out.InputTokens += data.InputTokens
		out.OutputTokens += data.OutputTokens
		out.TotalTokens += data.TotalTokens
	}
	return out
}

// LatestUsage returns the most recent usage event.
func LatestUsage(events []agentkit.SessionEvent) UsageData {
	var out UsageData
	for _, ev := range events {
		if ev.Type != agentkit.EventUsage {
			continue
		}
		var data UsageData
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		out = data
	}
	return out
}

// RepeatedToolCalls returns how many times the most recent tool call signature
// repeats consecutively at the tail of the log after seq.
func RepeatedToolCalls(events []agentkit.SessionEvent, seq agentkit.EventSeq) int {
	var signatures []string
	for _, ev := range events {
		if ev.Type != agentkit.EventToolCall || ev.Seq <= seq {
			continue
		}
		var call agentkit.ToolCall
		if err := json.Unmarshal(ev.Data, &call); err != nil {
			continue
		}
		signatures = append(signatures, call.Name+"\x00"+normalizeInput(call.Input))
	}
	if len(signatures) == 0 {
		return 0
	}
	last := signatures[len(signatures)-1]
	count := 0
	for i := len(signatures) - 1; i >= 0; i-- {
		if signatures[i] != last {
			break
		}
		count++
	}
	return count
}

func normalizeInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return strings.TrimSpace(string(raw))
	}
	out, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(out)
}
