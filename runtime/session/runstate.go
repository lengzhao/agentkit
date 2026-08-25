package session

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lengzhao/agentkit"
)

// Todo statuses. Anything other than TodoDone counts as outstanding work.
const (
	TodoPending    = "pending"
	TodoInProgress = "in_progress"
	TodoDone       = "done"
)

// Todo is one entry of the durable task list written by tool/todo.
type Todo struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// Done reports whether this entry no longer needs work.
func (t Todo) Done() bool { return t.Status == TodoDone }

// TodoUpdateData is the payload of an EventTodoUpdate event. It carries the full
// list after the update so the latest event alone reconstructs the state.
type TodoUpdateData struct {
	Items []Todo `json:"items"`
}

// RunFinishData is the payload of an EventRunFinish event: the agent's explicit
// statement that it is done, which is what stops an autonomous run.
type RunFinishData struct {
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

// Run finish statuses.
const (
	FinishCompleted = "completed"
	FinishBlocked   = "blocked"
)

// TurnContinueData records one autonomous turn extension. Messages holds the
// text injected to keep the agent going; derive replays it as a user message, so
// this single event is both the audit record and the model-visible source.
type TurnContinueData struct {
	Segment  int                     `json:"segment"`
	Reason   string                  `json:"reason"`
	Steps    int                     `json:"steps"`
	Messages []agentkit.ModelMessage `json:"messages,omitempty"`
}

// UsageData records token accounting for one model step.
type UsageData struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
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

// FinishAfter returns the run/finish event recorded after seq, or nil when the
// agent has not declared completion since then. Callers pass the seq of the
// message that started the current run so a stale finish cannot end a new run.
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

// LastAssistantText returns the text of the most recent assistant message after
// seq. It is the fallback answer for a run that stopped without calling
// tool/finish, which is the common case for a child agent that was only asked a
// question.
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

// StepCount counts the steps completed after seq. Unlike turn/end's Steps field
// it is also available for a turn that failed partway through.
func StepCount(events []agentkit.SessionEvent, seq agentkit.EventSeq) int {
	count := 0
	for _, ev := range events {
		if ev.Type == agentkit.EventStepEnd && ev.Seq > seq {
			count++
		}
	}
	return count
}

// RunStartSeq returns the seq of the most recent inbound user message, which
// marks where the current run began. Continuations are recorded as
// turn/continue events rather than user/message events, so they never shift it.
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

// LatestUsage returns the most recent usage event. Its InputTokens is the
// measured prompt size at that step — the best available reading of how large
// the context currently is, already net of any earlier compaction.
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
// repeats consecutively at the tail of the log, counting only calls after seq. A
// stuck agent calling the same tool with the same input is the cheapest stall
// signal available.
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
	// Re-marshal so key order and whitespace do not defeat the comparison.
	out, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(out)
}
