package agent

import "testing"

func TestBudgetStateRemainingContinuationsZeroWhenUnset(t *testing.T) {
	t.Parallel()

	b := newRunBudget(resolveBudgetSettings(nil), nil)
	state := b.state()
	if state.RemainingContinuations != 0 {
		t.Fatalf("RemainingContinuations = %d, want 0 when maxContinuations unset", state.RemainingContinuations)
	}
}

func TestBudgetStateRemainingContinuationsTracksLimit(t *testing.T) {
	t.Parallel()

	settings := resolveBudgetSettings(&BudgetConfig{MaxContinuations: 3})
	b := newRunBudget(settings, nil)
	b.recordContinuation()
	state := b.state()
	if state.RemainingContinuations != 2 {
		t.Fatalf("RemainingContinuations = %d, want 2", state.RemainingContinuations)
	}
}
