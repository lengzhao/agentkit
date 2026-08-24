package agent

import (
	"time"

	"github.com/lengzhao/agentkit"
)

// BudgetConfig bounds one autonomous turn. Every dimension is optional; zero
// means unlimited for that dimension. MaxContinuations defaults to 0, so an
// agent without explicit budget config behaves exactly as before: one segment,
// capped by MaxSteps.
type BudgetConfig struct {
	MaxTotalSteps    int     `json:"maxTotalSteps"`
	MaxContinuations int     `json:"maxContinuations"`
	WallClockSeconds int     `json:"wallClockSeconds"`
	MaxTotalTokens   int     `json:"maxTotalTokens"`
	SoftRatio        float64 `json:"softRatio"`
}

const defaultSoftRatio = 0.8

type budgetSettings struct {
	maxTotalSteps    int
	maxContinuations int
	wallClock        time.Duration
	maxTotalTokens   int
	softRatio        float64
}

func resolveBudgetSettings(cfg *BudgetConfig) budgetSettings {
	out := budgetSettings{softRatio: defaultSoftRatio}
	if cfg == nil {
		return out
	}
	out.maxTotalSteps = cfg.MaxTotalSteps
	out.maxContinuations = cfg.MaxContinuations
	out.maxTotalTokens = cfg.MaxTotalTokens
	if cfg.WallClockSeconds > 0 {
		out.wallClock = time.Duration(cfg.WallClockSeconds) * time.Second
	}
	if cfg.SoftRatio > 0 && cfg.SoftRatio < 1 {
		out.softRatio = cfg.SoftRatio
	}
	return out
}

// runBudget tracks consumption for one turn, including its continuations.
type runBudget struct {
	settings      budgetSettings
	started       time.Time
	now           func() time.Time
	steps         int
	continuations int
	tokens        int
}

func newRunBudget(settings budgetSettings, now func() time.Time) *runBudget {
	if now == nil {
		now = time.Now
	}
	return &runBudget{settings: settings, started: now(), now: now}
}

func (b *runBudget) recordStep()            { b.steps++ }
func (b *runBudget) recordContinuation()    { b.continuations++ }
func (b *runBudget) recordTokens(n int)     { b.tokens += n }
func (b *runBudget) stepsUsed() int         { return b.steps }
func (b *runBudget) continuationsUsed() int { return b.continuations }
func (b *runBudget) tokensUsed() int        { return b.tokens }

// allowsContinuation reports whether another segment may start. It is the hard
// gate: no hook can extend a turn past it.
func (b *runBudget) allowsContinuation() bool {
	if b.settings.maxContinuations <= 0 {
		return false
	}
	if b.continuations >= b.settings.maxContinuations {
		return false
	}
	return !b.hardExhausted()
}

func (b *runBudget) hardExhausted() bool {
	if b.settings.maxTotalSteps > 0 && b.steps >= b.settings.maxTotalSteps {
		return true
	}
	if b.settings.maxTotalTokens > 0 && b.tokens >= b.settings.maxTotalTokens {
		return true
	}
	if b.settings.wallClock > 0 && b.elapsed() >= b.settings.wallClock {
		return true
	}
	return false
}

func (b *runBudget) elapsed() time.Duration { return b.now().Sub(b.started) }

// stepsForSegment caps a segment's step allowance by whatever total steps remain.
func (b *runBudget) stepsForSegment(maxSteps int) int {
	if b.settings.maxTotalSteps <= 0 {
		return maxSteps
	}
	remaining := b.settings.maxTotalSteps - b.steps
	if remaining < 0 {
		remaining = 0
	}
	if remaining < maxSteps {
		return remaining
	}
	return maxSteps
}

// state renders the budget for TurnStopping hooks. Unlimited dimensions report
// -1 so a hook can tell "no limit" from "nothing left".
func (b *runBudget) state() agentkit.BudgetState {
	out := agentkit.BudgetState{
		RemainingSteps:         remaining(b.settings.maxTotalSteps, b.steps),
		RemainingContinuations: remaining(b.settings.maxContinuations, b.continuations),
		RemainingTokens:        remaining(b.settings.maxTotalTokens, b.tokens),
		RemainingSeconds:       -1,
		Exhausted:              b.hardExhausted(),
	}
	if b.settings.wallClock > 0 {
		left := int((b.settings.wallClock - b.elapsed()).Seconds())
		if left < 0 {
			left = 0
		}
		out.RemainingSeconds = left
	}
	out.SoftExhausted = b.softExhausted()
	return out
}

func (b *runBudget) softExhausted() bool {
	ratio := b.settings.softRatio
	if crossed(b.settings.maxTotalSteps, b.steps, ratio) {
		return true
	}
	if crossed(b.settings.maxContinuations, b.continuations, ratio) {
		return true
	}
	if crossed(b.settings.maxTotalTokens, b.tokens, ratio) {
		return true
	}
	if b.settings.wallClock > 0 {
		if float64(b.elapsed()) >= float64(b.settings.wallClock)*ratio {
			return true
		}
	}
	return false
}

func remaining(limit, used int) int {
	if limit <= 0 {
		return -1
	}
	if used >= limit {
		return 0
	}
	return limit - used
}

func crossed(limit, used int, ratio float64) bool {
	if limit <= 0 {
		return false
	}
	return float64(used) >= float64(limit)*ratio
}
