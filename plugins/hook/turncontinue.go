package hook

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type TurnContinueConfig struct {
	// MaxContinuations is the most segments this hook will ask for after the first.
	MaxContinuations int `json:"maxContinuations"`
	// ContinuePrompt is text injected to start another segment.
	ContinuePrompt string `json:"continuePrompt"`
	// RequireFinish keeps going until tool/finish is called, even with no pending todos.
	RequireFinish *bool `json:"requireFinish"`
	// RequireTodosDone keeps going while todos are still pending.
	RequireTodosDone *bool `json:"requireTodosDone"`
	// StallLimit stops after this many repeats of the same tool call signature.
	StallLimit int `json:"stallLimit"`
	// NoProgressLimit stops after this many consecutive continuations with no
	// substantive progress — no new tool/call, todo/update, or run/finish event
	// (default 3). Guards against idle loops when tool/finish is missing or the
	// model keeps ending segments with text-only replies.
	NoProgressLimit int `json:"noProgressLimit"`
}

type TurnContinueDeps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

const (
	defaultContinuePrompt  = "Keep going on the task. Review the remaining work, do the next concrete step, and call finish when everything is done or you are blocked."
	defaultStallLimit      = 3
	defaultNoProgressLimit = 3
)

type turnContinueProvider struct {
	cfg              TurnContinueConfig
	sessionStore     agentkit.SessionStore
	requireFinish    bool
	requireTodosDone bool
	progress         sync.Map // sessionID -> *progressTrack
}

// progressTrack tracks the substantive-event watermark across continuations.
type progressTrack struct {
	lastSeq    agentkit.EventSeq
	noProgress int
}

// SetDefaults implements pluginkit.Defaulter.
func (c *TurnContinueConfig) SetDefaults() {
	if c.ContinuePrompt == "" {
		c.ContinuePrompt = defaultContinuePrompt
	}
	if c.StallLimit == 0 {
		c.StallLimit = defaultStallLimit
	}
	if c.NoProgressLimit == 0 {
		c.NoProgressLimit = defaultNoProgressLimit
	}
}

// Validate implements pluginkit.Validator.
func (c *TurnContinueConfig) Validate() error {
	if c.MaxContinuations < 0 {
		return fmt.Errorf("hook/turn-continue maxContinuations must not be negative")
	}
	if c.StallLimit < 0 {
		return fmt.Errorf("hook/turn-continue stallLimit must not be negative")
	}
	if c.NoProgressLimit < 0 {
		return fmt.Errorf("hook/turn-continue noProgressLimit must not be negative")
	}
	return nil
}

// NewTurnContinue registers hook/turn-continue: Decide whether an autonomous turn continues or stops; contributes /status.
//
// Best practices:
//   - Useless without tool/todo and tool/finish: with no completion signal it can only stop on stall or continuation limit.
//   - Decision order is finish, then stall, then continuation limit, then no-progress, then pending work.
func NewTurnContinue(cfg TurnContinueConfig, deps TurnContinueDeps) (agentkit.HookProvider, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("hook/turn-continue requires sessionStore dependency")
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	requireFinish := true
	if cfg.RequireFinish != nil {
		requireFinish = *cfg.RequireFinish
	}
	requireTodosDone := true
	if cfg.RequireTodosDone != nil {
		requireTodosDone = *cfg.RequireTodosDone
	}
	return &turnContinueProvider{
		cfg:              cfg,
		sessionStore:     deps.SessionStore,
		requireFinish:    requireFinish,
		requireTodosDone: requireTodosDone,
	}, nil
}

func (p *turnContinueProvider) Hooks() []agentkit.Hook {
	return []agentkit.Hook{agentkit.OnTurnStopping(p.turnStopping)}
}

func (p *turnContinueProvider) Commands() []agentkit.Command {
	return []agentkit.Command{statusCommand{provider: p}}
}

func (p *turnContinueProvider) turnStopping(ctx context.Context, stopping *agentkit.TurnStopping) error {
	if p.cfg.MaxContinuations <= 0 {
		return nil
	}
	sessionID := rctx.SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil
	}
	events, err := loadRunEvents(ctx, p.sessionStore, sessionID)
	if err != nil {
		return err
	}
	state := capsession.RunStateFromEvents(events)

	if state.Finish != nil {
		stopping.Stop = true
		stopping.StopReason = "finished:" + state.Finish.Status
		return nil
	}
	if state.Repeats >= p.cfg.StallLimit {
		stopping.Stop = true
		stopping.StopReason = fmt.Sprintf("stalled: same tool call repeated %d times", state.Repeats)
		return nil
	}
	if stopping.Segments >= p.cfg.MaxContinuations {
		stopping.Stop = true
		stopping.StopReason = fmt.Sprintf("continuation limit reached (%d)", p.cfg.MaxContinuations)
		return nil
	}
	if p.noProgressStopped(sessionID, stopping, substantiveSeq(events)) {
		stopping.Stop = true
		stopping.StopReason = fmt.Sprintf("no progress: %d consecutive continuations without tool calls, todo updates, or finish", p.cfg.NoProgressLimit)
		return nil
	}
	if !p.wantsMoreWork(state) {
		stopping.Stop = true
		stopping.StopReason = "no outstanding work"
		return nil
	}

	stopping.Continue = append(stopping.Continue, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: p.continueText(stopping, state)}},
	})
	return nil
}

// substantiveSeq returns the highest seq of events that count as real work:
// tool calls, todo updates, or finish. Text-only replies do not move it.
func substantiveSeq(events []agentkit.SessionEvent) agentkit.EventSeq {
	var seq agentkit.EventSeq
	for _, ev := range events {
		switch ev.Type {
		case agentkit.EventToolCall, agentkit.EventTodoUpdate, agentkit.EventRunFinish:
			if ev.Seq > seq {
				seq = ev.Seq
			}
		}
	}
	return seq
}

// noProgressStopped tracks the substantive-event watermark across
// continuations and reports whether the run hit the no-progress limit.
// Segment 0 marks a fresh turn and resets the tracker.
func (p *turnContinueProvider) noProgressStopped(sessionID agentkit.SessionID, stopping *agentkit.TurnStopping, watermark agentkit.EventSeq) bool {
	if p.cfg.NoProgressLimit <= 0 {
		return false
	}
	track, _ := p.progress.LoadOrStore(sessionID, &progressTrack{})
	t := track.(*progressTrack)
	if stopping.Segments == 0 {
		t.lastSeq = watermark
		t.noProgress = 0
		return false
	}
	if watermark <= t.lastSeq {
		t.noProgress++
	} else {
		t.noProgress = 0
		t.lastSeq = watermark
	}
	return t.noProgress >= p.cfg.NoProgressLimit
}

// loadRunEvents reads the full session log for autonomous-run projections.
func loadRunEvents(ctx context.Context, store agentkit.SessionStore, sessionID agentkit.SessionID) ([]agentkit.SessionEvent, error) {
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return sess.Read(ctx, 0)
}

// loadRunState reads the session log and projects the autonomous-run signals.
func loadRunState(ctx context.Context, store agentkit.SessionStore, sessionID agentkit.SessionID) (capsession.RunState, error) {
	events, err := loadRunEvents(ctx, store, sessionID)
	if err != nil {
		return capsession.RunState{}, err
	}
	return capsession.RunStateFromEvents(events), nil
}

func (p *turnContinueProvider) wantsMoreWork(state capsession.RunState) bool {
	if p.requireTodosDone && len(state.Pending) > 0 {
		return true
	}
	return p.requireFinish
}

func (p *turnContinueProvider) continueText(_ *agentkit.TurnStopping, state capsession.RunState) string {
	var b strings.Builder
	b.WriteString(p.cfg.ContinuePrompt)
	if len(state.Pending) > 0 {
		b.WriteString("\n\nOutstanding tasks:")
		for _, item := range state.Pending {
			b.WriteString(fmt.Sprintf("\n- [%s] %s (id: %s)", item.Status, item.Title, item.ID))
		}
	} else if len(state.Todos) > 0 {
		b.WriteString("\n\nAll recorded tasks are done. If nothing remains, call finish.")
	}
	return b.String()
}

// statusCommand exposes run state for a long unattended run, where stdout has
// long since scrolled away.
type statusCommand struct {
	provider *turnContinueProvider
}

func (statusCommand) Name() string        { return "status" }
func (statusCommand) Alias() string       { return "" }
func (statusCommand) Description() string { return "show autonomous run state: tasks, usage, limits" }

func (c statusCommand) CommandExec(ctx context.Context, args string) (string, error) {
	if strings.TrimSpace(args) != "" {
		return "", fmt.Errorf("usage: /status")
	}
	sessionID := rctx.SessionIDFromContext(ctx)
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	state, err := loadRunState(ctx, c.provider.sessionStore, sessionID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "max continuations: %d\n", c.provider.cfg.MaxContinuations)
	fmt.Fprintf(&b, "tokens this run: %d (in %d / out %d)\n",
		state.Usage.TotalTokens, state.Usage.InputTokens, state.Usage.OutputTokens)
	fmt.Fprintf(&b, "context size (last measured): %d\n", state.Context)
	if state.Finish != nil {
		fmt.Fprintf(&b, "finished: %s — %s\n", state.Finish.Status, state.Finish.Summary)
	} else {
		b.WriteString("finished: no\n")
	}
	if len(state.Todos) == 0 {
		b.WriteString("tasks: none recorded")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "tasks: %d pending of %d\n", len(state.Pending), len(state.Todos))
	for _, item := range state.Todos {
		fmt.Fprintf(&b, "  [%s] %s (id: %s)\n", item.Status, item.Title, item.ID)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
