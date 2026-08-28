// Package telemetry defines the observability exporter boundary for AgentKit.
// Session JSONL remains the source of truth; exporters mirror turn/step/tool
// activity to external systems such as Langfuse.
package telemetry

import "context"

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
	Output string
	Err    error
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
}

// Usage carries token accounting for generations.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}
