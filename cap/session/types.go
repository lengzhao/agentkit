package session

import (
	"encoding/json"

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

// TurnSteerData is the payload of an EventTurnSteer event. The runner emits it
// on the outbound channel when an inbound message is folded into a running turn
// (steering) instead of starting a new one, so platforms can acknowledge the
// very message that was steered.
//
// The event carries the steered message's own route, not the running turn's: the
// point is to let the platform react on that message (Lark replaces its in-flight
// reaction emoji). Whether the agent later injects the message mid-turn or
// consumes it as the next turn is not visible here.
type TurnSteerData struct {
	// Outcome is always SteerOutcomeQueued today. Reserved as the seam for agents
	// that can report a finer result for their steering (injected mid-turn vs
	// deferred to the next turn) without a wire change.
	Outcome string `json:"outcome"`
	// SessionID is the conversation the running turn belongs to.
	SessionID string `json:"sessionId,omitempty"`
	// AgentID is the agent handling the running turn.
	AgentID string `json:"agentId,omitempty"`
	// UserID identifies the sender whose message was steered.
	UserID string `json:"userId,omitempty"`
	// Preview is a bounded, human-readable excerpt of the steered message.
	Preview string `json:"preview,omitempty"`
}

// The outcome vocabulary of an EventTurnSteer event. The runner reports queued
// the moment a message is accepted; whoever resolves where it actually landed
// reports the terminal outcome through the message's SteerAck.
const (
	// SteerOutcomeQueued means the message was accepted onto the running
	// session's steer queue. It is the immediate, best-effort ack.
	SteerOutcomeQueued = "queued"
	// SteerOutcomeInjected means the message was folded into the turn that was
	// already streaming (a local agent injected it at its next step boundary, or
	// an ACP peer acknowledged `_session/steering`).
	SteerOutcomeInjected = "injected"
	// SteerOutcomeNextTurn means the running turn was too far along to accept
	// steering, so the message runs as the session's next turn instead.
	SteerOutcomeNextTurn = "next-turn"
	// SteerOutcomeRejected means delivery failed. The message may or may not have
	// reached the agent, so it is never retried automatically.
	SteerOutcomeRejected = "rejected"
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
	// Message carries the turn's final assistant message when one was produced.
	// It lets consumers render the reply without depending on streaming deltas
	// (message/update) having all arrived.
	Message json.RawMessage `json:"message,omitempty"`
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
