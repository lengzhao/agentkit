package rctx

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// SubagentModelScope carries the parent turn's agent id into a delegated child
// context. In-process subagent models resolve from explicit config only —
// the definition model, then global binds keyed by the child agent, then by the
// parent agent — never from conversation session state.
type SubagentModelScope struct {
	ParentAgentID agentkit.AgentID
}

type subagentModelScopeKey struct{}

// WithSubagentModelScope attaches the parent model scope to a child context.
func WithSubagentModelScope(ctx context.Context, scope SubagentModelScope) context.Context {
	return context.WithValue(ctx, subagentModelScopeKey{}, scope)
}

// SubagentModelScopeFrom returns the parent model scope, or false when the
// context is not running inside an in-process subagent delegation.
func SubagentModelScopeFrom(ctx context.Context) (SubagentModelScope, bool) {
	scope, ok := ctx.Value(subagentModelScopeKey{}).(SubagentModelScope)
	return scope, ok
}
