package agent

// turnMeter counts consumption for one turn (all segments). Limits are enforced
// by TurnStopping hooks, not the agent runtime.
type turnMeter struct {
	steps         int
	continuations int
	tokens        int
}

func newTurnMeter() *turnMeter {
	return &turnMeter{}
}

func (m *turnMeter) recordStep()            { m.steps++ }
func (m *turnMeter) recordContinuation()    { m.continuations++ }
func (m *turnMeter) recordTokens(n int)     { m.tokens += n }
func (m *turnMeter) stepsUsed() int         { return m.steps }
func (m *turnMeter) continuationsUsed() int { return m.continuations }
func (m *turnMeter) tokensUsed() int        { return m.tokens }
