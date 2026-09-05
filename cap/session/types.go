package session

import "github.com/lengzhao/agentkit"

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

// TodoUpdateData is the payload of an EventTodoUpdate event.
type TodoUpdateData struct {
	Items []Todo `json:"items"`
}

// RunFinishData is the payload of an EventRunFinish event.
type RunFinishData struct {
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

// Run finish statuses.
const (
	FinishCompleted = "completed"
	FinishBlocked   = "blocked"
)

// TurnContinueData records one autonomous turn extension.
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

// RunState is a snapshot of autonomous-run signals from the session log.
type RunState struct {
	StartSeq agentkit.EventSeq
	Todos    []Todo
	Pending  []Todo
	Finish   *RunFinishData
	Repeats  int
	Usage    UsageData
	Context  int
}

// DeliveryParts holds parsed segments of a platform delivery SessionID.
type DeliveryParts struct {
	Platform string
	Channel  string
	Thread   string
	User     string
	Routable bool
}

// MetadataLogicalChars stores pre-sanitize message size on user/assistant events.
const MetadataLogicalChars = "logical_chars"
