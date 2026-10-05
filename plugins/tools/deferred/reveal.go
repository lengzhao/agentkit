package deferred

import (
	"context"

	"github.com/lengzhao/agentkit/runtime/rctx"
)

// stateKeyRevealed is the turn-state key for the revealed tool name set.
const stateKeyRevealed = "tools/deferred.revealed"

// maxRevealedPerTurn caps how many searched/described tools stay fully visible
// within one turn; beyond it the earliest revealed names drop back to bridge-only.
const maxRevealedPerTurn = 24

// reveal marks names as fully visible for the rest of this turn.
// No-op when the runner injected no turn state (stateless degradation).
func reveal(ctx context.Context, names ...string) {
	st := rctx.StateFrom(ctx)
	if st == nil || len(names) == 0 {
		return
	}
	currentVal, _ := st.Get(stateKeyRevealed)
	current, _ := currentVal.([]string)
	seen := make(map[string]bool, len(current)+len(names))
	out := make([]string, 0, len(current)+len(names))
	// Newest first so truncation drops the earliest revealed.
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, name := range current {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) > maxRevealedPerTurn {
		out = out[:maxRevealedPerTurn]
	}
	st.Set(stateKeyRevealed, out)
}

// revealedNames returns the turn's revealed set (nil when empty or stateless).
func revealedNames(ctx context.Context) map[string]bool {
	st := rctx.StateFrom(ctx)
	if st == nil {
		return nil
	}
	namesVal, _ := st.Get(stateKeyRevealed)
	names, _ := namesVal.([]string)
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}
