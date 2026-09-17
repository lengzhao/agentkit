package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// stubTurnStopping drives the turn-stopping seam directly, so these tests cover
// the agent's contract rather than any particular driver plugin.
type stubTurnStopping struct {
	continueTexts []string
	forceStop     bool
	stopReason    string
	seen          []agentkit.TurnStopping
}

func (h *stubTurnStopping) BeforeStep(context.Context, *agentkit.BeforeStep) error { return nil }
func (h *stubTurnStopping) BeforeTool(context.Context, *agentkit.ToolCall) error   { return nil }
func (h *stubTurnStopping) AfterTool(context.Context, *agentkit.ToolResult) error  { return nil }

func (h *stubTurnStopping) TurnComplete(context.Context, *agentkit.TurnComplete) error { return nil }

func (h *stubTurnStopping) TurnStopping(_ context.Context, in *agentkit.TurnStopping) error {
	h.seen = append(h.seen, *in)
	if h.forceStop {
		in.Stop = true
		in.StopReason = h.stopReason
	}
	if len(h.continueTexts) > 0 {
		text := h.continueTexts[0]
		h.continueTexts = h.continueTexts[1:]
		in.Continue = append(in.Continue, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		})
	}
	return nil
}

type turnFixture struct {
	agent     agentkit.Agent
	store     agentkit.SessionStore
	sessionID agentkit.SessionID
}

// textReplies scripts an LLM that answers with plain text every step, so each
// segment ends immediately with StopNoToolCalls.
func textReplies(n int) []llm.ScriptedStep {
	steps := make([]llm.ScriptedStep, 0, n)
	for i := 0; i < n; i++ {
		steps = append(steps, llm.ScriptedStep{Text: "reply"})
	}
	return steps
}

// readReplies scripts an LLM that always calls the read tool, so a segment can
// only end on its step limit.
func readReplies(n int) []llm.ScriptedStep {
	steps := make([]llm.ScriptedStep, 0, n)
	for i := 0; i < n; i++ {
		steps = append(steps, llm.ScriptedStep{
			Text:      "reading",
			ToolCalls: []agentkit.ToolCall{llm.MustToolCall("read", `{"path":"README.md"}`)},
		})
	}
	return steps
}

func newTurnFixture(t *testing.T, hooks agentkit.HookRuntime, cfg agent.Config, steps []llm.ScriptedStep) turnFixture {
	t.Helper()
	dir := t.TempDir()

	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	readPack, err := fs.NewFSMemory(fs.FSMemoryConfig{
		Files: map[string]string{"README.md": "hello"},
		Tools: []string{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{ToolPacks: []agentkit.ToolPack{readPack}})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}

	ag, err := agent.New(cfg, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRT,
		Prompt:       assembler,
		Hooks:        hooks,
		Workspace:    rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}
	return turnFixture{agent: ag, store: store, sessionID: agentkit.SessionID("test:turnstop")}
}

func (f turnFixture) run(t *testing.T) error {
	t.Helper()
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(f.sessionID), Workspace: string(f.sessionID)})
	return f.agent.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "go"}},
		},
	})
}

func (f turnFixture) sessionEvents(t *testing.T) []agentkit.SessionEvent {
	t.Helper()
	sess, err := f.store.Get(context.Background(), f.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := session.ReadAllEvents(context.Background(), sess)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	return events
}

func countEvents(events []agentkit.SessionEvent, typ agentkit.EventType) int {
	n := 0
	for _, ev := range events {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

func TestTurnStoppingContinueExtendsTurn(t *testing.T) {
	t.Parallel()

	hooks := &stubTurnStopping{continueTexts: []string{"keep going"}}
	f := newTurnFixture(t, hooks, agent.Config{
		ID:       "test",
		MaxSteps: 5,
		Budget:   &agent.BudgetConfig{MaxContinuations: 3},
	}, textReplies(4))
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	events := f.sessionEvents(t)
	if got := countEvents(events, agentkit.EventTurnContinue); got != 1 {
		t.Fatalf("turn/continue events = %d, want 1", got)
	}
	// One step per model reply: the original segment plus the extension.
	if got := countEvents(events, agentkit.EventStepStart); got != 2 {
		t.Fatalf("step/start events = %d, want 2", got)
	}
	if len(hooks.seen) != 2 {
		t.Fatalf("turn-stopping calls = %d, want 2", len(hooks.seen))
	}
	if hooks.seen[0].Reason != agentkit.StopNoToolCalls {
		t.Fatalf("first stop reason = %q, want %q", hooks.seen[0].Reason, agentkit.StopNoToolCalls)
	}
	if hooks.seen[1].Segments != 1 {
		t.Fatalf("second call Segments = %d, want 1", hooks.seen[1].Segments)
	}

	var data session.TurnContinueData
	for _, ev := range events {
		if ev.Type == agentkit.EventTurnContinue {
			if err := json.Unmarshal(ev.Data, &data); err != nil {
				t.Fatalf("decode turn/continue: %v", err)
			}
		}
	}
	if len(data.Messages) != 1 {
		t.Fatalf("turn/continue messages = %d, want 1", len(data.Messages))
	}
	if got := data.Messages[0].Content[0].Text; got != "keep going" {
		t.Fatalf("injected text = %q, want %q", got, "keep going")
	}
	if data.Reason != string(agentkit.StopNoToolCalls) {
		t.Fatalf("turn/continue reason = %q, want %q", data.Reason, agentkit.StopNoToolCalls)
	}
}

func TestTurnContinueIsModelVisible(t *testing.T) {
	t.Parallel()

	hooks := &stubTurnStopping{continueTexts: []string{"keep going"}}
	f := newTurnFixture(t, hooks, agent.Config{
		ID:       "test",
		MaxSteps: 5,
		Budget:   &agent.BudgetConfig{MaxContinuations: 1},
	}, textReplies(4))
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	// The next segment's history must contain the injected message: a
	// continuation the model cannot see would silently do nothing.
	if len(hooks.seen) < 2 {
		t.Fatalf("turn-stopping calls = %d, want at least 2", len(hooks.seen))
	}
	found := false
	for _, msg := range hooks.seen[1].Messages {
		for _, part := range msg.Content {
			if part.Text == "keep going" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("injected continuation missing from derived history: %+v", hooks.seen[1].Messages)
	}
}

func TestHardBudgetOverridesHookContinue(t *testing.T) {
	t.Parallel()

	// The hook asks for three continuations; maxTotalSteps allows two steps total.
	hooks := &stubTurnStopping{continueTexts: []string{"more", "more", "more"}}
	f := newTurnFixture(t, hooks, agent.Config{
		ID:       "test",
		MaxSteps: 1,
		Budget:   &agent.BudgetConfig{MaxContinuations: 10, MaxTotalSteps: 2},
	}, textReplies(6))
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	events := f.sessionEvents(t)
	if got := countEvents(events, agentkit.EventStepStart); got != 2 {
		t.Fatalf("step/start events = %d, want 2 (maxTotalSteps)", got)
	}
	if got := countEvents(events, agentkit.EventTurnContinue); got != 1 {
		t.Fatalf("turn/continue events = %d, want 1", got)
	}
	last := hooks.seen[len(hooks.seen)-1]
	if !last.Budget.Exhausted {
		t.Fatalf("final budget should be exhausted: %+v", last.Budget)
	}
	if last.Reason != agentkit.StopBudget {
		t.Fatalf("final stop reason = %q, want %q", last.Reason, agentkit.StopBudget)
	}
}

func TestTurnStoppingStopWinsOverContinue(t *testing.T) {
	t.Parallel()

	hooks := &stubTurnStopping{
		continueTexts: []string{"more"},
		forceStop:     true,
		stopReason:    "finished",
	}
	f := newTurnFixture(t, hooks, agent.Config{
		ID:       "test",
		MaxSteps: 5,
		Budget:   &agent.BudgetConfig{MaxContinuations: 5},
	}, textReplies(4))
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	events := f.sessionEvents(t)
	if got := countEvents(events, agentkit.EventTurnContinue); got != 0 {
		t.Fatalf("turn/continue events = %d, want 0 when Stop is set", got)
	}
	if got := countEvents(events, agentkit.EventStepStart); got != 1 {
		t.Fatalf("step/start events = %d, want 1", got)
	}
}

func TestWithoutBudgetTurnStaysSingleSegment(t *testing.T) {
	t.Parallel()

	// No budget config: a hook asking to continue must not change the old
	// behaviour, since maxContinuations defaults to 0.
	hooks := &stubTurnStopping{continueTexts: []string{"more", "more"}}
	f := newTurnFixture(t, hooks, agent.Config{ID: "test", MaxSteps: 5}, textReplies(4))
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	events := f.sessionEvents(t)
	if got := countEvents(events, agentkit.EventTurnContinue); got != 0 {
		t.Fatalf("turn/continue events = %d, want 0 without budget", got)
	}
	if got := countEvents(events, agentkit.EventStepStart); got != 1 {
		t.Fatalf("step/start events = %d, want 1", got)
	}
}

func TestSoftBudgetIsReportedToHooks(t *testing.T) {
	t.Parallel()

	// 4 continuations allowed, softRatio 0.5: the third checkpoint (2 used) has
	// crossed the soft threshold.
	hooks := &stubTurnStopping{continueTexts: []string{"a", "b", "c"}}
	f := newTurnFixture(t, hooks, agent.Config{
		ID:       "test",
		MaxSteps: 5,
		Budget:   &agent.BudgetConfig{MaxContinuations: 4, SoftRatio: 0.5},
	}, textReplies(6))
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if len(hooks.seen) < 3 {
		t.Fatalf("turn-stopping calls = %d, want at least 3", len(hooks.seen))
	}
	if hooks.seen[0].Budget.SoftExhausted {
		t.Fatal("first checkpoint should be below the soft threshold")
	}
	if !hooks.seen[2].Budget.SoftExhausted {
		t.Fatalf("third checkpoint should be soft-exhausted: %+v", hooks.seen[2].Budget)
	}
}

func TestStepLimitReasonReachesHooks(t *testing.T) {
	t.Parallel()

	hooks := &stubTurnStopping{continueTexts: []string{"keep going"}}
	f := newTurnFixture(t, hooks, agent.Config{
		ID:       "test",
		MaxSteps: 1,
		Budget:   &agent.BudgetConfig{MaxContinuations: 1},
	}, readReplies(3))
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	if len(hooks.seen) == 0 {
		t.Fatal("expected turn-stopping to be called")
	}
	if hooks.seen[0].Reason != agentkit.StopStepLimit {
		t.Fatalf("stop reason = %q, want %q", hooks.seen[0].Reason, agentkit.StopStepLimit)
	}
}

// TestLimitSummaryOnStepLimit verifies that a turn ending on the per-segment
// step limit runs one final, tool-less LLM step so the user sees a result
// instead of a truncated transcript. No hook and no budget are configured:
// this is the default-on, tool-free path.
func TestLimitSummaryOnStepLimit(t *testing.T) {
	t.Parallel()

	// One read step fills the segment (MaxSteps=1 -> StopStepLimit); the next
	// scripted step is the summary reply.
	steps := append(readReplies(1), llm.ScriptedStep{Text: "here is the summary"})
	f := newTurnFixture(t, nil, agent.Config{
		ID:       "test",
		MaxSteps: 1,
	}, steps)
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	events := f.sessionEvents(t)
	// The work step runs; the summary step must NOT emit step/start (it is
	// teardown, not a budgeted work step).
	if got := countEvents(events, agentkit.EventStepStart); got != 1 {
		t.Fatalf("step/start events = %d, want 1 (summary is not a work step)", got)
	}
	// One turn/continue records the injected summary prompt.
	continues := eventsOfType(events, agentkit.EventTurnContinue)
	if len(continues) != 1 {
		t.Fatalf("turn/continue events = %d, want 1", len(continues))
	}
	var data session.TurnContinueData
	if err := json.Unmarshal(continues[0].Data, &data); err != nil {
		t.Fatalf("decode turn/continue: %v", err)
	}
	if data.Reason != "summary:"+string(agentkit.StopStepLimit) {
		t.Fatalf("turn/continue reason = %q, want summary:%s", data.Reason, agentkit.StopStepLimit)
	}
	// The last assistant message is the summary and carries no tool calls.
	assistants := eventsOfType(events, agentkit.EventAssistantMessage)
	if len(assistants) != 2 {
		t.Fatalf("assistant/message events = %d, want 2 (work + summary)", len(assistants))
	}
	var lastMsg agentkit.ModelMessage
	if err := json.Unmarshal(assistants[1].Data, &lastMsg); err != nil {
		t.Fatalf("decode last assistant message: %v", err)
	}
	if len(lastMsg.ToolCalls) != 0 {
		t.Fatalf("summary message has %d tool calls, want 0", len(lastMsg.ToolCalls))
	}
	if text := textOf(lastMsg.Content); text != "here is the summary" {
		t.Fatalf("summary text = %q, want %q", text, "here is the summary")
	}
}

// TestLimitSummaryDisabledWhenOptedOut confirms DisableSummaryOnLimit=true keeps
// the old behaviour: a truncated turn ends with no extra LLM call.
func TestLimitSummaryDisabledWhenOptedOut(t *testing.T) {
	t.Parallel()

	steps := append(readReplies(1), llm.ScriptedStep{Text: "should not happen"})
	f := newTurnFixture(t, nil, agent.Config{
		ID:                    "test",
		MaxSteps:              1,
		DisableSummaryOnLimit: true,
	}, steps)
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	events := f.sessionEvents(t)
	if got := countEvents(events, agentkit.EventTurnContinue); got != 0 {
		t.Fatalf("turn/continue events = %d, want 0 when summary disabled", got)
	}
	// Only the work step's assistant message; the unused summary step must not
	// be consumed.
	if got := countEvents(events, agentkit.EventAssistantMessage); got != 1 {
		t.Fatalf("assistant/message events = %d, want 1", got)
	}
}

// TestLimitSummaryOnBudgetExhaustion drives the turn through continuations
// until the hard step budget denies another segment; the summary then fires
// even though the budget is spent (the summary step is not charged).
func TestLimitSummaryOnBudgetExhaustion(t *testing.T) {
	t.Parallel()

	// Hook keeps asking to continue; maxTotalSteps=2 stops it after two reads.
	hooks := &stubTurnStopping{continueTexts: []string{"more", "more", "more"}}
	steps := append(readReplies(2), llm.ScriptedStep{Text: "final summary"})
	f := newTurnFixture(t, hooks, agent.Config{
		ID:       "test",
		MaxSteps: 1,
		Budget:   &agent.BudgetConfig{MaxContinuations: 10, MaxTotalSteps: 2},
	}, steps)
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	events := f.sessionEvents(t)
	if got := countEvents(events, agentkit.EventStepStart); got != 2 {
		t.Fatalf("step/start events = %d, want 2 work steps", got)
	}
	// One real continuation plus the summary continuation.
	continues := eventsOfType(events, agentkit.EventTurnContinue)
	if len(continues) != 2 {
		t.Fatalf("turn/continue events = %d, want 2 (1 real + 1 summary)", len(continues))
	}
	// The summary continuation must not reuse the real continuation's segment
	// number: real continuations are 1-indexed after recordContinuation, so the
	// summary takes the next notional segment (here 2).
	var realData session.TurnContinueData
	if err := json.Unmarshal(continues[0].Data, &realData); err != nil {
		t.Fatalf("decode real turn/continue: %v", err)
	}
	var summaryData session.TurnContinueData
	if err := json.Unmarshal(continues[1].Data, &summaryData); err != nil {
		t.Fatalf("decode summary turn/continue: %v", err)
	}
	if summaryData.Segment == realData.Segment {
		t.Fatalf("summary segment %d collides with real continuation segment %d",
			summaryData.Segment, realData.Segment)
	}
	// The turn ended because the hard step budget (maxTotalSteps=2) was
	// exhausted, so the summary's audit reason must reflect StopBudget, not the
	// StopStepLimit that the last segment returned.
	if summaryData.Reason != "summary:"+string(agentkit.StopBudget) {
		t.Fatalf("last turn/continue reason = %q, want summary:%s", summaryData.Reason, agentkit.StopBudget)
	}
	// The final assistant message is the summary, tool-free.
	assistants := eventsOfType(events, agentkit.EventAssistantMessage)
	if len(assistants) != 3 {
		t.Fatalf("assistant/message events = %d, want 3 (2 work + 1 summary)", len(assistants))
	}
	var lastMsg agentkit.ModelMessage
	if err := json.Unmarshal(assistants[2].Data, &lastMsg); err != nil {
		t.Fatalf("decode last assistant message: %v", err)
	}
	if len(lastMsg.ToolCalls) != 0 {
		t.Fatalf("summary message has %d tool calls, want 0", len(lastMsg.ToolCalls))
	}
}

// TestLimitSummarySuppressedWhenHookForcesStop confirms that when a hook
// deliberately stops the turn (Stop set, e.g. finished/stalled), the agent does
// not add a limit summary: the hook already decided how the turn ends.
func TestLimitSummarySuppressedWhenHookForcesStop(t *testing.T) {
	t.Parallel()

	hooks := &stubTurnStopping{forceStop: true, stopReason: "stalled"}
	// One read step ends the segment on StopStepLimit; a second scripted step
	// would be the summary, which must NOT run because the hook forced Stop.
	steps := append(readReplies(1), llm.ScriptedStep{Text: "should not happen"})
	f := newTurnFixture(t, hooks, agent.Config{
		ID:       "test",
		MaxSteps: 1,
		Budget:   &agent.BudgetConfig{MaxContinuations: 5},
	}, steps)
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	events := f.sessionEvents(t)
	if got := countEvents(events, agentkit.EventTurnContinue); got != 0 {
		t.Fatalf("turn/continue events = %d, want 0 when hook forces Stop", got)
	}
	if got := countEvents(events, agentkit.EventAssistantMessage); got != 1 {
		t.Fatalf("assistant/message events = %d, want 1 (no summary)", got)
	}
}

// TestLimitSummaryFailureDoesNotFailTurn confirms the summary is best-effort:
// when its LLM call fails (here: the scripted provider is exhausted), the turn
// still completes successfully with its original stop reason instead of being
// retroactively marked failed. The work step's assistant message is recorded;
// no summary assistant message is produced.
func TestLimitSummaryFailureDoesNotFailTurn(t *testing.T) {
	t.Parallel()

	// Exactly one read step fills the segment (MaxSteps=1 -> StopStepLimit).
	// The summary then asks the scripted LLM for another step, but the
	// provider is exhausted -> Stream returns a non-cancellation error, which
	// the agent must swallow rather than fail the turn on.
	f := newTurnFixture(t, nil, agent.Config{
		ID:       "test",
		MaxSteps: 1,
	}, readReplies(1))
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v (summary failure must not fail the turn)", err)
	}

	events := f.sessionEvents(t)
	// The work step runs; the summary LLM step never produces a message.
	if got := countEvents(events, agentkit.EventStepStart); got != 1 {
		t.Fatalf("step/start events = %d, want 1 (work only)", got)
	}
	if got := countEvents(events, agentkit.EventAssistantMessage); got != 1 {
		t.Fatalf("assistant/message events = %d, want 1 (no summary message on failure)", got)
	}
	// The summary prompt is recorded as a turn/continue before the LLM call
	// fails, so it remains in history regardless.
	if got := countEvents(events, agentkit.EventTurnContinue); got != 1 {
		t.Fatalf("turn/continue events = %d, want 1 (summary prompt appended before failure)", got)
	}
}

// eventsOfType filters session events by type.
func eventsOfType(events []agentkit.SessionEvent, typ agentkit.EventType) []agentkit.SessionEvent {
	var out []agentkit.SessionEvent
	for _, ev := range events {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

// textOf returns the concatenated text parts of a message.
func textOf(parts []agentkit.ContentPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}
