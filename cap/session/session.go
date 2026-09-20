// Package session is the capability boundary for the session event log:
// event payload DTOs, pure projections over the recorded events, and the
// write/index interfaces implemented by runtime/session/sessevents and
// injected into plugins via deps.
package session

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/cap/skill"
)

// Transcript records model-visible conversation events.
type Transcript interface {
	// AppendMessage sanitizes and appends a user/assistant message event.
	AppendMessage(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, typ agentkit.EventType, msg agentkit.ModelMessage) error
	// AppendToolCall sanitizes and appends a tool call event.
	AppendToolCall(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, call agentkit.ToolCall) error
	// AppendToolResult appends a tool result event.
	AppendToolResult(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, result agentkit.ToolResult) error
}

// Lifecycle records turn and step bracketing.
type Lifecycle interface {
	AppendTurnStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID) error
	AppendTurnEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data TurnEndData) error
	AppendStepStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, step int) error
	AppendStepEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, step int) error
}

// RunLog records agent-run control events (todo, finish, continue, usage).
type RunLog interface {
	AppendTodoUpdate(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, items []Todo) error
	AppendRunFinish(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data RunFinishData) error
	AppendTurnContinue(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data TurnContinueData) error
	AppendUsage(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data UsageData) error
}

// Compaction records compaction markers, indexes history for summarization,
// and logs summarization-retry attempts around the summary LLM call.
type Compaction interface {
	// AppendCompaction writes a compaction marker event. Compaction events are
	// self-describing: session backends trim their own in-memory history when
	// appending one, and message derivation always honors the latest marker.
	AppendCompaction(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data compaction.EventData) error
	AppendSummarizationRetryStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data RetryStartData) error
	AppendSummarizationRetryEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data RetryEndData) error
	// IndexForCompaction rebuilds the model-visible message list with source
	// event seqs, including the latest compaction summary and retained tail.
	IndexForCompaction(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID) ([]compaction.IndexedMessage, error)
}

// Skills records skill injections during tool execution.
type Skills interface {
	AppendSkillLoad(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, content skill.Content) error
}

// Conversation is transcript plus turn bracketing, used by remote agents that
// persist inbound/outbound messages without owning the local tool loop.
type Conversation interface {
	Transcript
	Lifecycle
}

// Events is the full session event log write contract. The standard
// implementation lives in runtime/session/sessevents (kind session/events).
// Plugins should depend on the smallest interface they need; the same
// session.events instance satisfies all of them.
type Events interface {
	Transcript
	Lifecycle
	RunLog
	Compaction
	Skills
	// Recovery markers: overflow and generic auto-retry bracketing. No plugin
	// consumes these today; runtime records them through the same instance.
	AppendAutoRetryStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data RetryStartData) error
	AppendAutoRetryEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data RetryEndData) error
	AppendOverflowRecovery(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data OverflowRecoveryData) error
}
