package hook_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/hook"
	"github.com/lengzhao/agentkit/runtime/session"
)

type singleSessionStore struct {
	sess agentkit.Session
}

func (s singleSessionStore) Get(context.Context, agentkit.SessionID) (agentkit.Session, error) {
	return s.sess, nil
}

func newDriver(t *testing.T, cfg hook.TurnContinueConfig) (agentkit.TurnStoppingHook, agentkit.Session) {
	t.Helper()
	sess, err := session.NewMemory(session.MemoryConfig{ID: "test:driver"})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := hook.NewTurnContinue(cfg, hook.TurnContinueDeps{SessionStore: singleSessionStore{sess: sess}})
	if err != nil {
		t.Fatalf("build hook/turn-continue: %v", err)
	}
	hooks := provider.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("hooks = %d, want 1", len(hooks))
	}
	h, ok := hooks[0].(agentkit.TurnStoppingHook)
	if !ok {
		t.Fatal("hook does not implement TurnStoppingHook")
	}
	return h, sess
}

func driverCtx(sess agentkit.Session) context.Context {
	return session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(sess.ID()), Workspace: string(sess.ID())})
}

// startRun records the inbound user message that marks the run's beginning.
func startRun(t *testing.T, sess agentkit.Session) {
	t.Helper()
	if err := session.AppendMessage(context.Background(), sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "do the task"}},
	}); err != nil {
		t.Fatal(err)
	}
}

func stopping() *agentkit.TurnStopping {
	return &agentkit.TurnStopping{
		Reason: agentkit.StopNoToolCalls,
		Steps:  3,
		Budget: agentkit.BudgetState{
			RemainingSteps:         -1,
			RemainingContinuations: 4,
			RemainingSeconds:       -1,
			RemainingTokens:        -1,
		},
	}
}

func TestDriverContinuesWhileTodosPending(t *testing.T) {
	t.Parallel()

	h, sess := newDriver(t, hook.TurnContinueConfig{MaxContinuations: 5})
	startRun(t, sess)
	if err := session.AppendTodoUpdate(context.Background(), sess, "a", []session.Todo{
		{ID: "1", Title: "write the parser", Status: session.TodoInProgress},
		{ID: "2", Title: "add tests", Status: session.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}

	in := stopping()
	if err := h.TurnStopping(driverCtx(sess), in); err != nil {
		t.Fatalf("turn stopping: %v", err)
	}
	if in.Stop {
		t.Fatalf("should not stop with pending todos: %s", in.StopReason)
	}
	if len(in.Continue) != 1 {
		t.Fatalf("continue messages = %d, want 1", len(in.Continue))
	}
	text := in.Continue[0].Content[0].Text
	for _, want := range []string{"write the parser", "add tests", "4 continuation(s) left"} {
		if !strings.Contains(text, want) {
			t.Fatalf("continuation text missing %q:\n%s", want, text)
		}
	}
}

func TestDriverStopsAfterFinish(t *testing.T) {
	t.Parallel()

	h, sess := newDriver(t, hook.TurnContinueConfig{MaxContinuations: 5})
	startRun(t, sess)
	// Work remains, but the agent declared the run over: finish wins.
	if err := session.AppendTodoUpdate(context.Background(), sess, "a", []session.Todo{
		{ID: "1", Title: "unfinished", Status: session.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendRunFinish(context.Background(), sess, "a", session.RunFinishData{
		Status:  session.FinishBlocked,
		Summary: "needs credentials",
	}); err != nil {
		t.Fatal(err)
	}

	in := stopping()
	if err := h.TurnStopping(driverCtx(sess), in); err != nil {
		t.Fatalf("turn stopping: %v", err)
	}
	if !in.Stop {
		t.Fatal("expected stop after finish")
	}
	if len(in.Continue) != 0 {
		t.Fatalf("continue messages = %d, want 0", len(in.Continue))
	}
	if !strings.Contains(in.StopReason, session.FinishBlocked) {
		t.Fatalf("stop reason = %q, want it to mention %q", in.StopReason, session.FinishBlocked)
	}
}

func TestDriverStopsOnStall(t *testing.T) {
	t.Parallel()

	h, sess := newDriver(t, hook.TurnContinueConfig{MaxContinuations: 5, StallLimit: 3})
	startRun(t, sess)
	if err := session.AppendTodoUpdate(context.Background(), sess, "a", []session.Todo{
		{ID: "1", Title: "still pending", Status: session.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := session.AppendToolCall(context.Background(), sess, "a", agentkit.ToolCall{
			ID:    "call",
			Name:  "read",
			Input: []byte(`{"path":"same.go"}`),
		}); err != nil {
			t.Fatal(err)
		}
	}

	in := stopping()
	if err := h.TurnStopping(driverCtx(sess), in); err != nil {
		t.Fatalf("turn stopping: %v", err)
	}
	if !in.Stop {
		t.Fatal("expected stop on stall")
	}
	if !strings.Contains(in.StopReason, "stalled") {
		t.Fatalf("stop reason = %q, want it to mention a stall", in.StopReason)
	}
}

func TestDriverStopsAtContinuationLimit(t *testing.T) {
	t.Parallel()

	h, sess := newDriver(t, hook.TurnContinueConfig{MaxContinuations: 2})
	startRun(t, sess)
	if err := session.AppendTodoUpdate(context.Background(), sess, "a", []session.Todo{
		{ID: "1", Title: "still pending", Status: session.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}

	in := stopping()
	in.Segments = 2
	if err := h.TurnStopping(driverCtx(sess), in); err != nil {
		t.Fatalf("turn stopping: %v", err)
	}
	if !in.Stop {
		t.Fatal("expected stop at the continuation limit")
	}
	if !strings.Contains(in.StopReason, "continuation limit") {
		t.Fatalf("stop reason = %q", in.StopReason)
	}
}

func TestDriverIsInertWithoutMaxContinuations(t *testing.T) {
	t.Parallel()

	// The default config must not make an existing agent autonomous.
	h, sess := newDriver(t, hook.TurnContinueConfig{})
	startRun(t, sess)
	if err := session.AppendTodoUpdate(context.Background(), sess, "a", []session.Todo{
		{ID: "1", Title: "still pending", Status: session.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}

	in := stopping()
	if err := h.TurnStopping(driverCtx(sess), in); err != nil {
		t.Fatalf("turn stopping: %v", err)
	}
	if in.Stop || len(in.Continue) != 0 {
		t.Fatalf("driver should be inert: stop=%v continue=%d", in.Stop, len(in.Continue))
	}
}

func TestDriverDefersToExhaustedBudget(t *testing.T) {
	t.Parallel()

	h, sess := newDriver(t, hook.TurnContinueConfig{MaxContinuations: 5})
	startRun(t, sess)
	if err := session.AppendTodoUpdate(context.Background(), sess, "a", []session.Todo{
		{ID: "1", Title: "still pending", Status: session.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}

	in := stopping()
	in.Reason = agentkit.StopBudget
	in.Budget.Exhausted = true
	if err := h.TurnStopping(driverCtx(sess), in); err != nil {
		t.Fatalf("turn stopping: %v", err)
	}
	if len(in.Continue) != 0 {
		t.Fatalf("continue messages = %d, want 0 once the budget is spent", len(in.Continue))
	}
}

func TestDriverInjectsWrapUpWhenSoftExhausted(t *testing.T) {
	t.Parallel()

	h, sess := newDriver(t, hook.TurnContinueConfig{
		MaxContinuations: 5,
		ContinuePrompt:   "CONTINUE-MARKER",
		WrapUpPrompt:     "WRAPUP-MARKER",
	})
	startRun(t, sess)
	if err := session.AppendTodoUpdate(context.Background(), sess, "a", []session.Todo{
		{ID: "1", Title: "still pending", Status: session.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}

	in := stopping()
	in.Budget.SoftExhausted = true
	if err := h.TurnStopping(driverCtx(sess), in); err != nil {
		t.Fatalf("turn stopping: %v", err)
	}
	if len(in.Continue) != 1 {
		t.Fatalf("continue messages = %d, want 1", len(in.Continue))
	}
	text := in.Continue[0].Content[0].Text
	if !strings.Contains(text, "WRAPUP-MARKER") || strings.Contains(text, "CONTINUE-MARKER") {
		t.Fatalf("expected the wrap-up prompt, got:\n%s", text)
	}
}

func TestStatusCommandReportsRunState(t *testing.T) {
	t.Parallel()

	sess, err := session.NewMemory(session.MemoryConfig{ID: "test:status"})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := hook.NewTurnContinue(
		hook.TurnContinueConfig{MaxContinuations: 7},
		hook.TurnContinueDeps{SessionStore: singleSessionStore{sess: sess}},
	)
	if err != nil {
		t.Fatal(err)
	}
	commands, ok := provider.(agentkit.CommandProvider)
	if !ok {
		t.Fatal("hook/turn-continue should contribute commands")
	}
	var status agentkit.Command
	for _, cmd := range commands.Commands() {
		if cmd.Name() == "status" {
			status = cmd
		}
	}
	if status == nil {
		t.Fatal("no /status command contributed")
	}

	startRun(t, sess)
	if err := session.AppendTodoUpdate(context.Background(), sess, "a", []session.Todo{
		{ID: "1", Title: "done thing", Status: session.TodoDone},
		{ID: "2", Title: "pending thing", Status: session.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendUsage(context.Background(), sess, "a", session.UsageData{
		InputTokens: 100, OutputTokens: 20, TotalTokens: 120,
	}); err != nil {
		t.Fatal(err)
	}

	out, err := status.CommandExec(driverCtx(sess), "")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"max continuations: 7", "tokens this run: 120", "1 pending of 2", "pending thing"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
}
