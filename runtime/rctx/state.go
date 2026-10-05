package rctx

import (
	"context"
	"sync"

	"github.com/lengzhao/agentkit"
)

// State is a mutable per-turn bag injected by the runner at turn start.
// Hooks, tools, and policies share it for optimization-grade state
// (caches, reveal sets, markers); it is destroyed with the turn.
//
// Keys use the "namespace.key" convention, namespace = plugin kind
// (e.g. "tools/deferred.revealed"). State must never affect correctness:
// plugins degrade to stateless behavior when StateFrom returns nil.
type State struct {
	mu sync.RWMutex
	kv map[string]any
}

// NewState returns an empty turn state bag.
func NewState() *State {
	return &State{kv: make(map[string]any)}
}

// WithState attaches a state bag to ctx, typically once at turn start.
func WithState(ctx context.Context, s *State) context.Context {
	if s == nil {
		return ctx
	}
	return context.WithValue(ctx, agentkit.KeyTurnState, s)
}

// StateFrom returns the turn state bag, or nil when none was injected.
func StateFrom(ctx context.Context) *State {
	s, _ := ctx.Value(agentkit.KeyTurnState).(*State)
	return s
}

// Get returns the value for key, or (nil, false) when absent.
func (s *State) Get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.kv[key]
	return v, ok
}

// Set stores v under key, replacing any previous value.
func (s *State) Set(key string, v any) {
	s.mu.Lock()
	s.kv[key] = v
	s.mu.Unlock()
}

// Delete removes key.
func (s *State) Delete(key string) {
	s.mu.Lock()
	delete(s.kv, key)
	s.mu.Unlock()
}
