// Package telemetry defines the observability exporter boundary for AgentKit.
// Session JSONL remains the source of truth; exporters mirror turn/step/tool
// activity to external systems such as Langfuse.
package telemetry

import (
	"context"
	"time"
)

// Exporter mirrors agent activity to an external observability backend.
type Exporter interface {
	BeginTurn(ctx context.Context, meta TurnMeta) (context.Context, func(TurnEnd))
	BeginObservation(ctx context.Context, meta ObservationMeta) (context.Context, func(ObservationEnd))
	RecordEvent(ctx context.Context, name string, attrs map[string]string)
	Flush(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// TurnMeta describes one agent RunTurn boundary.
type TurnMeta struct {
	TurnID            string
	SessionID         string
	DeliverySessionID string
	AgentID           string
	PlatformID        string
	UserID            string
	Input             string
}

// TurnEnd closes a turn trace.
type TurnEnd struct {
	Output     string
	Err        error
	Usage      *Usage
	Steps      int
	StopReason string
}

// ObservationKind selects how an observation is exported.
type ObservationKind string

const (
	KindSpan       ObservationKind = "span"
	KindGeneration ObservationKind = "generation"
	KindTool       ObservationKind = "tool"
)

// ObservationMeta describes a nested observation such as an LLM call or tool.
type ObservationMeta struct {
	Name  string
	Kind  ObservationKind
	Model string
	Input string
	// ToolNames lists model-visible tools for an LLM generation.
	ToolNames []string
	// AgentID labels which agent produced this observation.
	AgentID string
	// SessionID labels the session that produced this observation.
	SessionID string
	// Scope marks a span that should parent nested generations (e.g. subagent.turn).
	Scope bool
}

// ObservationEnd closes an observation.
type ObservationEnd struct {
	Output string
	Err    error
	Usage  *Usage
	// CompletionStartTime is when the model first produced output (TTFT boundary).
	// Zero means unavailable, e.g. the stream ended before any content arrived.
	CompletionStartTime time.Time
	// FirstTextTime is when the first user-visible text or thinking delta arrived.
	// Langfuse TTFT prefers this over CompletionStartTime when set.
	FirstTextTime time.Time
}

// Usage carries token accounting for generations and turn totals.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}
