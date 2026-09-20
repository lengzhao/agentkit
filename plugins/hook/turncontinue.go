package hook

import (
	"context"
	"fmt"
	"strings"

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
}

type TurnContinueDeps struct {
	SessionStore agentkit.SessionStore `json:"sessionStore"`
}

const (
	defaultContinuePrompt = "Keep going on the task. Review the remaining work, do the next concrete step, and call finish when everything is done or you are blocked."
	defaultStallLimit     = 3
)

type turnContinueProvider struct {
	cfg              TurnContinueConfig
	sessionStore     agentkit.SessionStore
	requireFinish    bool
	requireTodosDone bool
}

// NewTurnContinue registers hook/turn-continue: Decide whether an autonomous turn continues or stops; contributes /status.
//
// Best practices:
//   - Useless without tool/todo and tool/finish: with no completion signal it can only stop on stall or continuation limit.
//   - Decision order is finish, then stall, then continuation limit, then pending work.
func NewTurnContinue(cfg TurnContinueConfig, deps TurnContinueDeps) (agentkit.HookProvider, error) {
	if deps.SessionStore == nil {
		return nil, fmt.Errorf("hook/turn-continue requires sessionStore dependency")
	}
	if cfg.MaxContinuations < 0 {
		return nil, fmt.Errorf("hook/turn-continue maxContinuations must not be negative")
	}
	if cfg.ContinuePrompt == "" {
		cfg.ContinuePrompt = defaultContinuePrompt
	}
	if cfg.StallLimit <= 0 {
		cfg.StallLimit = defaultStallLimit
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
	state, err := loadRunState(ctx, p.sessionStore, sessionID)
	if err != nil {
		return err
	}

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

// loadRunState reads the session log and projects the autonomous-run signals.
func loadRunState(ctx context.Context, store agentkit.SessionStore, sessionID agentkit.SessionID) (capsession.RunState, error) {
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		return capsession.RunState{}, err
	}
	events, err := sess.Read(ctx, 0)
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
