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
	// ToolCallInterval injects a budget-status notice (numbers only) after every N
	// completed tool calls in a turn (default 20). 0 keeps the default; set a very
	// large value to disable.
	ToolCallInterval int `json:"toolCallInterval"`
	// TimeIntervalMinutes injects a budget-status notice (numbers only) after every
	// N minutes of turn elapsed time (default 10). 0 keeps the default; set a very
	// large value to disable.
	TimeIntervalMinutes int `json:"timeIntervalMinutes"`
	// ToolCallLimit is the tool-call budget for one turn (default 60). Once reached,
	// every further tool result carries LimitMessage telling the model to summarize
	// and end the turn. 0 keeps the default; set a very large value to disable.
	ToolCallLimit int `json:"toolCallLimit"`
	// TimeLimitMinutes is the elapsed-time budget for one turn (default 60). Once
	// reached, every further tool result carries LimitMessage. 0 keeps the default;
	// set a very large value to disable.
	TimeLimitMinutes int `json:"timeLimitMinutes"`
	// Message is the periodic budget-status template (numbers only, no wrap-up
	// directive); {toolCalls} and {elapsed} are replaced.
	Message string `json:"message"`
	// LimitMessage is injected on every tool result after a limit is reached;
	// {toolCalls} and {elapsed} are replaced.
	LimitMessage string `json:"limitMessage"`
}

// TurnNudgeDeps has no dependencies; state is kept in memory per session.
type TurnNudgeDeps struct{}

const (
	defaultToolCallInterval    = 20
	defaultTimeIntervalMinutes = 10
	defaultToolCallLimit       = 60
	defaultTimeLimitMinutes    = 60
	defaultNudgeMessage        = "[turn-nudge] Budget status: {toolCalls} tool calls, {elapsed} elapsed this turn."
	defaultLimitMessage        = "[turn-nudge] LIMIT REACHED: this turn has used {toolCalls} tool calls over {elapsed}, hitting the configured budget. Do not start any new work. Summarize what is done and what remains, then end the turn now (call finish if available)."
)

// SetDefaults implements pluginkit.Defaulter.
func (c *TurnNudgeConfig) SetDefaults() {
	if c.ToolCallInterval == 0 {
		c.ToolCallInterval = defaultToolCallInterval
	}
	if c.TimeIntervalMinutes == 0 {
		c.TimeIntervalMinutes = defaultTimeIntervalMinutes
	}
	if c.ToolCallLimit == 0 {
		c.ToolCallLimit = defaultToolCallLimit
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
	if c.ToolCallInterval < 0 {
		return fmt.Errorf("hook/turn-nudge toolCallInterval must not be negative")
	}
	if c.TimeIntervalMinutes < 0 {
		return fmt.Errorf("hook/turn-nudge timeIntervalMinutes must not be negative")
	}
	if c.ToolCallLimit < 0 {
		return fmt.Errorf("hook/turn-nudge toolCallLimit must not be negative")
	}
	if c.TimeLimitMinutes < 0 {
		return fmt.Errorf("hook/turn-nudge timeLimitMinutes must not be negative")
	}
	return nil
}

// turnNudgeState tracks one turn's consumption; reset at each turn start.
type turnNudgeState struct {
	startedAt   time.Time
	toolCalls   int
	nextCountAt int
	nextTimeAt  time.Time
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

// NewTurnNudge registers hook/turn-nudge: Inject wrap-up notices into tool results as a turn consumes its tool-call / elapsed-time budget.
//
// Best practices:
//   - Notices are appended to tool results, so they persist in session history and reach the model at the next step.
//   - Two tiers: periodic soft nudges (toolCallInterval / timeIntervalMinutes) and a budget limit (toolCallLimit / timeLimitMinutes) after which every tool result carries limitMessage.
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

// beforeStep resets per-turn counters at the first step of a turn.
func (p *turnNudgeProvider) beforeStep(ctx context.Context, step *agentkit.BeforeStep) error {
	if step.Step != 0 || step.Segment != 0 {
		return nil
	}
	sessionID := rctx.SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil
	}
	p.mu.Lock()
	p.turns[sessionID] = p.newState(p.now())
	p.mu.Unlock()
	return nil
}

// afterTool counts the completed call and appends a notice when either budget
// interval has elapsed since the last notice.
func (p *turnNudgeProvider) afterTool(ctx context.Context, result *agentkit.ToolResult) error {
	sessionID := rctx.SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil
	}
	notice := p.recordToolAndNudge(sessionID)
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
		startedAt:   now,
		nextCountAt: p.cfg.ToolCallInterval,
		nextTimeAt:  now.Add(p.timeInterval),
	}
}

func (p *turnNudgeProvider) recordToolAndNudge(sessionID agentkit.SessionID) string {
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	st, ok := p.turns[sessionID]
	if !ok {
		st = p.newState(now)
		p.turns[sessionID] = st
	}
	st.toolCalls++
	elapsed := now.Sub(st.startedAt).Round(time.Second)

	// Limit tier: once any budget is reached, every further tool result carries
	// the wrap-up-and-end message; periodic nudges are superseded.
	if st.toolCalls >= p.cfg.ToolCallLimit || elapsed >= p.timeLimit {
		return p.render(p.cfg.LimitMessage, st.toolCalls, elapsed)
	}

	nudge := false
	if st.toolCalls >= st.nextCountAt {
		nudge = true
		st.nextCountAt = st.toolCalls + p.cfg.ToolCallInterval
	}
	if !now.Before(st.nextTimeAt) {
		nudge = true
		st.nextTimeAt = now.Add(p.timeInterval)
	}
	if !nudge {
		return ""
	}
	return p.render(p.cfg.Message, st.toolCalls, elapsed)
}

func (p *turnNudgeProvider) render(template string, toolCalls int, elapsed time.Duration) string {
	return strings.NewReplacer(
		"{toolCalls}", strconv.Itoa(toolCalls),
		"{elapsed}", elapsed.String(),
	).Replace(template)
}
