package hook

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

type TurnContinueConfig struct {
	// MaxContinuations is segments this hook will ask for. The agent's budget.maxContinuations is the hard ceiling and always wins.
	MaxContinuations int `json:"maxContinuations"`
	// ContinuePrompt is text injected to start another segment.
	ContinuePrompt string `json:"continuePrompt"`
	// WrapUpPrompt is injected instead of ContinuePrompt once the budget is softly exhausted.
	WrapUpPrompt string `json:"wrapUpPrompt"`
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
	defaultWrapUpPrompt   = "The run budget is nearly spent. Wrap up now: finish what can be completed safely, then call finish with a summary of what is done and what remains."
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
//   - Useless without tool/todo and tool/finish: with no completion signal it can only stop on budget or stall.
//   - Decision order is finish, then stall, then budget, then segment limit, then pending work.
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
	if cfg.WrapUpPrompt == "" {
		cfg.WrapUpPrompt = defaultWrapUpPrompt
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
	sessionID := session.SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil
	}
	state, err := session.LoadRunState(ctx, p.sessionStore, sessionID)
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
	if stopping.Budget.Exhausted {
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

func (p *turnContinueProvider) wantsMoreWork(state session.RunState) bool {
	if p.requireTodosDone && len(state.Pending) > 0 {
		return true
	}
	return p.requireFinish
}

func (p *turnContinueProvider) continueText(stopping *agentkit.TurnStopping, state session.RunState) string {
	var b strings.Builder
	if stopping.Budget.SoftExhausted {
		b.WriteString(p.cfg.WrapUpPrompt)
	} else {
		b.WriteString(p.cfg.ContinuePrompt)
	}
	if stopping.Reason == agentkit.StopStepLimit {
		b.WriteString("\n\nThe previous segment hit its step limit mid-task.")
	}
	if len(state.Pending) > 0 {
		b.WriteString("\n\nOutstanding tasks:")
		for _, item := range state.Pending {
			b.WriteString(fmt.Sprintf("\n- [%s] %s (id: %s)", item.Status, item.Title, item.ID))
		}
	} else if len(state.Todos) > 0 {
		b.WriteString("\n\nAll recorded tasks are done. If nothing remains, call finish.")
	}
	b.WriteString(fmt.Sprintf("\n\nBudget at this checkpoint: %s.", describeBudget(stopping.Budget)))
	return b.String()
}

func describeBudget(state agentkit.BudgetState) string {
	parts := make([]string, 0, 4)
	if state.RemainingContinuations >= 0 {
		parts = append(parts, fmt.Sprintf("%d continuation(s) left", state.RemainingContinuations))
	}
	if state.RemainingSteps >= 0 {
		parts = append(parts, fmt.Sprintf("%d step(s) left", state.RemainingSteps))
	}
	if state.RemainingSeconds >= 0 {
		parts = append(parts, fmt.Sprintf("%ds left", state.RemainingSeconds))
	}
	if state.RemainingTokens >= 0 {
		parts = append(parts, fmt.Sprintf("%d token(s) left", state.RemainingTokens))
	}
	if len(parts) == 0 {
		return "no configured limits"
	}
	return strings.Join(parts, ", ")
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
	sessionID := session.SessionIDFromContext(ctx)
	if sessionID == "" {
		return "", fmt.Errorf("session id is required")
	}
	state, err := session.LoadRunState(ctx, c.provider.sessionStore, sessionID)
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