package agent

// turnMeter counts consumption for one turn (all segments). Step caps are
// enforced by the agent maxSteps config; token/continuation policy uses hooks.
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

// stepPosition identifies the step about to run: step is the 0-based,
// turn-wide index matching step/start events; segment is the 0-based
// continuation index matching turn/continue's Segment numbering.
type stepPosition struct {
	step    int
	segment int
}
