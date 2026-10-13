package hook

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/pluginkit"
)

type TurnNudgeConfig struct {
	// StepInterval injects a budget-status notice (numbers only) after every N
	// completed model steps in a turn (default 20). Steps align with agent maxSteps:
	// one step may batch many tool calls. 0 keeps the default; set a very large
	// value to disable.
	StepInterval int `json:"stepInterval"`
	// TimeIntervalMinutes injects a budget-status notice (numbers only) after every
	// N minutes of turn elapsed time (default 10). 0 keeps the default; set a very
	// large value to disable.
	TimeIntervalMinutes int `json:"timeIntervalMinutes"`
	// StepLimit is the model-step budget for one turn (default 100). Once reached,
	// every further tool result carries LimitMessage telling the model to summarize
	// and end the turn. 0 keeps the default; set a very large value to disable.
	StepLimit int `json:"stepLimit"`
	// TimeLimitMinutes is the elapsed-time budget for one turn (default 60). Once
	// reached, every further tool result carries LimitMessage. 0 keeps the default;
	// set a very large value to disable.
	TimeLimitMinutes int `json:"timeLimitMinutes"`
	// Message is the periodic budget-status template (numbers only, no wrap-up
	// directive); {steps} and {elapsed} are replaced.
	Message string `json:"message"`
	// LimitMessage is injected on every tool result after a limit is reached;
	// {steps} and {elapsed} are replaced.
	LimitMessage string `json:"limitMessage"`
}

// TurnNudgeDeps has no dependencies; state is kept in memory per session.
type TurnNudgeDeps struct{}

const (
	defaultStepInterval        = 20
	defaultTimeIntervalMinutes = 10
	defaultStepLimit           = 100
	defaultTimeLimitMinutes    = 60
	defaultNudgeMessage        = "[turn-nudge] Budget status: {steps} steps, {elapsed} elapsed this turn."
	defaultLimitMessage        = "[turn-nudge] LIMIT REACHED: this turn has used {steps} steps over {elapsed}, hitting the configured budget. Do not start any new work. Summarize what is done and what remains, then end the turn now (call finish if available)."
)

// SetDefaults implements pluginkit.Defaulter.
func (c *TurnNudgeConfig) SetDefaults() {
	if c.StepInterval == 0 {
		c.StepInterval = defaultStepInterval
	}
	if c.TimeIntervalMinutes == 0 {
		c.TimeIntervalMinutes = defaultTimeIntervalMinutes
	}
	if c.StepLimit == 0 {
		c.StepLimit = defaultStepLimit
	}
	if c.TimeLimitMinutes == 0 {
		c.TimeLimitMinutes = defaultTimeLimitMinutes
	}
	if c.Message == "" {
		c.Message = defaultNudgeMessage
	}
	if c.LimitMessage == "" {
		c.LimitMessage = defaultLimitMessage
	}
}

// Validate implements pluginkit.Validator.
func (c *TurnNudgeConfig) Validate() error {
	if c.StepInterval < 0 {
		return fmt.Errorf("hook/turn-nudge stepInterval must not be negative")
	}
	if c.TimeIntervalMinutes < 0 {
		return fmt.Errorf("hook/turn-nudge timeIntervalMinutes must not be negative")
	}
	if c.StepLimit < 0 {
		return fmt.Errorf("hook/turn-nudge stepLimit must not be negative")
	}
	if c.TimeLimitMinutes < 0 {
		return fmt.Errorf("hook/turn-nudge timeLimitMinutes must not be negative")
	}
	return nil
}

// turnNudgeState tracks one turn's consumption; reset at each turn start.
type turnNudgeState struct {
	startedAt time.Time
	// steps is the number of completed model steps, mirrored from BeforeStep.Step.
	steps int
	// nextStepNudge is the steps value at which the next periodic notice is due.
	nextStepNudge int
	// lastNudgeStep guards the periodic tier against injecting into every tool
	// result of the same step (parallel batches share one steps value).
	lastNudgeStep int
	nextTimeAt    time.Time
}

type turnNudgeProvider struct {
	cfg          TurnNudgeConfig
	timeInterval time.Duration
	timeLimit    time.Duration
	now          func() time.Time

	mu    sync.Mutex
	turns map[agentkit.SessionID]*turnNudgeState
}

func init() {
	pluginkit.Register("hook/turn-nudge", NewTurnNudge)
}

// NewTurnNudge registers hook/turn-nudge: Inject wrap-up notices into tool results as a turn consumes its step / elapsed-time budget.
//
// Best practices:
//   - Notices are appended to tool results, so they persist in session history and reach the model at the next step.
//   - Two tiers: periodic soft nudges (stepInterval / timeIntervalMinutes) and a budget limit (stepLimit / timeLimitMinutes) after which every tool result carries limitMessage.
//   - The step counter mirrors agent maxSteps semantics (turn-wide, across segments); one parallel tool batch still counts as one step.
//   - This hook only nudges; hard limits stay with agent maxSteps and hook/turn-continue.
func NewTurnNudge(cfg TurnNudgeConfig, _ TurnNudgeDeps) (agentkit.HookProvider, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &turnNudgeProvider{
		cfg:          cfg,
		timeInterval: time.Duration(cfg.TimeIntervalMinutes) * time.Minute,
		timeLimit:    time.Duration(cfg.TimeLimitMinutes) * time.Minute,
		now:          time.Now,
		turns:        make(map[agentkit.SessionID]*turnNudgeState),
	}, nil
}

func (p *turnNudgeProvider) Hooks() agentkit.HookContribution {
	return agentkit.HookContribution{
		BeforeStep: agentkit.OnBeforeStep(p.beforeStep),
		AfterTool:  agentkit.OnAfterTool(p.afterTool),
	}
}

// beforeStep resets per-turn counters at the first step of a turn and mirrors
// the completed-step count thereafter (Step is the 0-based index of the step
// about to run, i.e. the number of steps completed so far).
func (p *turnNudgeProvider) beforeStep(ctx context.Context, step *agentkit.BeforeStep) error {
	sessionID := rctx.SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if step.Step == 0 && step.Segment == 0 {
		p.turns[sessionID] = p.newState(p.now())
		return nil
	}
	if st, ok := p.turns[sessionID]; ok {
		st.steps = step.Step
	}
	return nil
}

// afterTool appends a notice when either budget interval has elapsed since the
// last notice, or the wrap-up-and-end limit message once a limit is reached.
func (p *turnNudgeProvider) afterTool(ctx context.Context, result *agentkit.ToolResult) error {
	sessionID := rctx.SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil
	}
	notice := p.nudge(sessionID)
	if notice == "" {
		return nil
	}
	if result.Content != "" {
		result.Content += "\n\n"
	}
	result.Content += notice
	return nil
}

func (p *turnNudgeProvider) newState(now time.Time) *turnNudgeState {
	return &turnNudgeState{
		startedAt:     now,
		nextStepNudge: p.cfg.StepInterval,
		lastNudgeStep: -1,
		nextTimeAt:    now.Add(p.timeInterval),
	}
}

func (p *turnNudgeProvider) nudge(sessionID agentkit.SessionID) string {
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	st, ok := p.turns[sessionID]
	if !ok {
		st = p.newState(now)
		p.turns[sessionID] = st
	}
	elapsed := now.Sub(st.startedAt).Round(time.Second)

	// Limit tier: once any budget is reached, every further tool result carries
	// the wrap-up-and-end message; periodic nudges are superseded.
	if st.steps >= p.cfg.StepLimit || elapsed >= p.timeLimit {
		return p.render(p.cfg.LimitMessage, st.steps, elapsed)
	}

	nudge := false
	if st.steps >= st.nextStepNudge && st.steps != st.lastNudgeStep {
		nudge = true
		st.lastNudgeStep = st.steps
		st.nextStepNudge = st.steps + p.cfg.StepInterval
	}
	if !now.Before(st.nextTimeAt) {
		nudge = true
		st.nextTimeAt = now.Add(p.timeInterval)
	}
	if !nudge {
		return ""
	}
	return p.render(p.cfg.Message, st.steps, elapsed)
}

func (p *turnNudgeProvider) render(template string, steps int, elapsed time.Duration) string {
	return strings.NewReplacer(
		"{steps}", strconv.Itoa(steps),
		"{elapsed}", elapsed.String(),
	).Replace(template)
}
