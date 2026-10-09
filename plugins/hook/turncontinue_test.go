package hook_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/plugins/hook"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
)

type singleSessionStore struct {
	sess agentkit.Session
}

func (s singleSessionStore) Get(context.Context, agentkit.SessionID) (agentkit.Session, error) {
	return s.sess, nil
}

func newDriver(t *testing.T, cfg hook.TurnContinueConfig) (agentkit.TurnStoppingHook, agentkit.Session) {
	t.Helper()
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:driver"})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := hook.NewTurnContinue(cfg, hook.TurnContinueDeps{SessionStore: singleSessionStore{sess: sess}})
	if err != nil {
		t.Fatalf("build hook/turn-continue: %v", err)
	}
	hooks := provider.Hooks()
	if hooks.TurnStopping == nil {
		t.Fatal("expected TurnStopping hook")
	}
	return hooks.TurnStopping, sess
}

func driverCtx(sess agentkit.Session) context.Context {
	return rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(sess.ID()), Workspace: string(sess.ID())})
}

// startRun records the inbound user message that marks the run's beginning.
func startRun(t *testing.T, sess agentkit.Session) {
	t.Helper()
	if err := sessevents.Default.AppendMessage(context.Background(), sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
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
	}
}

func TestDriverContinuesWhileTodosPending(t *testing.T) {
	t.Parallel()

	h, sess := newDriver(t, hook.TurnContinueConfig{MaxContinuations: 5})
	startRun(t, sess)
	if err := sessevents.Default.AppendTodoUpdate(context.Background(), sess, "a", []capsession.Todo{
		{ID: "1", Title: "write the parser", Status: capsession.TodoInProgress},
		{ID: "2", Title: "add tests", Status: capsession.TodoPending},
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
	for _, want := range []string{"write the parser", "add tests"} {
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
	if err := sessevents.Default.AppendTodoUpdate(context.Background(), sess, "a", []capsession.Todo{
		{ID: "1", Title: "unfinished", Status: capsession.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendRunFinish(context.Background(), sess, "a", capsession.RunFinishData{
		Status:  capsession.FinishBlocked,
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
	if !strings.Contains(in.StopReason, capsession.FinishBlocked) {
		t.Fatalf("stop reason = %q, want it to mention %q", in.StopReason, capsession.FinishBlocked)
	}
}

func TestDriverStopsOnStall(t *testing.T) {
	t.Parallel()

	h, sess := newDriver(t, hook.TurnContinueConfig{MaxContinuations: 5, StallLimit: 3})
	startRun(t, sess)
	if err := sessevents.Default.AppendTodoUpdate(context.Background(), sess, "a", []capsession.Todo{
		{ID: "1", Title: "still pending", Status: capsession.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := sessevents.Default.AppendToolCall(context.Background(), sess, "a", agentkit.ToolCall{
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
	if err := sessevents.Default.AppendTodoUpdate(context.Background(), sess, "a", []capsession.Todo{
		{ID: "1", Title: "still pending", Status: capsession.TodoPending},
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

func TestDriverStopsOnNoProgress(t *testing.T) {
	t.Parallel()

	h, sess := newDriver(t, hook.TurnContinueConfig{MaxContinuations: 10, NoProgressLimit: 3})
	startRun(t, sess)
	if err := sessevents.Default.AppendTodoUpdate(context.Background(), sess, "a", []capsession.Todo{
		{ID: "1", Title: "still pending", Status: capsession.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}

	call := func(seg int) *agentkit.TurnStopping {
		in := stopping()
		in.Segments = seg
		if err := h.TurnStopping(driverCtx(sess), in); err != nil {
			t.Fatal(err)
		}
		return in
	}
	// Segment 0: baseline (the todo update above is the initial watermark).
	if in := call(0); in.Stop {
		t.Fatalf("segment 0 should continue: %s", in.StopReason)
	}
	// Continuations with text-only replies (no new tool/todo/finish events).
	for seg := 1; seg <= 2; seg++ {
		if in := call(seg); in.Stop {
			t.Fatalf("segment %d should continue: %s", seg, in.StopReason)
		}
	}
	in := call(3)
	if !in.Stop {
		t.Fatal("expected stop on no progress")
	}
	if !strings.Contains(in.StopReason, "no progress") {
		t.Fatalf("stop reason = %q", in.StopReason)
	}
}

func TestDriverNoProgressResetsOnSubstantiveEvents(t *testing.T) {
	t.Parallel()

	h, sess := newDriver(t, hook.TurnContinueConfig{MaxContinuations: 10, NoProgressLimit: 2})
	startRun(t, sess)
	if err := sessevents.Default.AppendTodoUpdate(context.Background(), sess, "a", []capsession.Todo{
		{ID: "1", Title: "still pending", Status: capsession.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}

	call := func(seg int) *agentkit.TurnStopping {
		in := stopping()
		in.Segments = seg
		if err := h.TurnStopping(driverCtx(sess), in); err != nil {
			t.Fatal(err)
		}
		return in
	}
	call(0)                     // baseline
	if in := call(1); in.Stop { // no progress #1
		t.Fatalf("segment 1 should continue: %s", in.StopReason)
	}
	// The segment did real work: a tool call moves the watermark, counter resets.
	if err := sessevents.Default.AppendToolCall(context.Background(), sess, "a", agentkit.ToolCall{
		ID: "c1", Name: "read", Input: []byte(`{"path":"a.go"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if in := call(2); in.Stop {
		t.Fatalf("segment 2 should continue: %s", in.StopReason)
	}
	if in := call(3); in.Stop { // no progress #1 again
		t.Fatalf("segment 3 should continue: %s", in.StopReason)
	}
	in := call(4) // no progress #2: limit reached
	if !in.Stop || !strings.Contains(in.StopReason, "no progress") {
		t.Fatalf("segment 4: stop=%v reason=%q", in.Stop, in.StopReason)
	}
}

func TestDriverIsInertWithoutMaxContinuations(t *testing.T) {
	t.Parallel()

	// The default config must not make an existing agent autonomous.
	h, sess := newDriver(t, hook.TurnContinueConfig{})
	startRun(t, sess)
	if err := sessevents.Default.AppendTodoUpdate(context.Background(), sess, "a", []capsession.Todo{
		{ID: "1", Title: "still pending", Status: capsession.TodoPending},
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

func TestStatusCommandReportsRunState(t *testing.T) {
	t.Parallel()

	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:status"})
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
	if err := sessevents.Default.AppendTodoUpdate(context.Background(), sess, "a", []capsession.Todo{
		{ID: "1", Title: "done thing", Status: capsession.TodoDone},
		{ID: "2", Title: "pending thing", Status: capsession.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendUsage(context.Background(), sess, "a", capsession.UsageData{
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
