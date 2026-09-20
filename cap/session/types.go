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

// MetadataLogicalChars stores pre-sanitize size when storage shrinks the message (e.g. stripped inline media).
const MetadataLogicalChars = "logical_chars"

type TurnStartData struct{}

type TurnEndData struct {
	Steps      int    `json:"steps"`
	StopReason string `json:"stopReason,omitempty"`
	StepLimit  int    `json:"stepLimit,omitempty"`
	Cancelled  bool   `json:"cancelled,omitempty"`
	Failed     bool   `json:"failed,omitempty"`
}

type StepStartData struct {
	Step int `json:"step"`
}

type StepEndData struct {
	Step int `json:"step"`
}

// RetryStartData is the shared payload of the retry-bracketing start events
// (auto-retry, summarization-retry); the event type distinguishes the domain.
type RetryStartData struct {
	Attempt      int    `json:"attempt"`
	MaxAttempts  int    `json:"maxAttempts"`
	DelayMs      int    `json:"delayMs"`
	ErrorMessage string `json:"errorMessage"`
}

// RetryEndData is the shared payload of the retry-bracketing end events.
type RetryEndData struct {
	Success    bool   `json:"success"`
	Attempt    int    `json:"attempt"`
	FinalError string `json:"finalError,omitempty"`
}

type OverflowRecoveryData struct {
	Applied int    `json:"applied"`
	Reason  string `json:"reason,omitempty"`
	Error   string `json:"error,omitempty"`
}

// RecoveryData is the audit payload of a session/recovery event, written after
// repairing an interrupted turn.
type RecoveryData struct {
	TurnStartSeq  agentkit.EventSeq `json:"turnStartSeq"`
	Steps         int               `json:"steps"`
	OrphanResults int               `json:"orphanResults"`
	ClosedStep    int               `json:"closedStep"`
	Reason        string            `json:"reason"`
}
