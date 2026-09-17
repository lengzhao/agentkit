package agent_test

import (
	"context"
	"encoding/json"
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
	f := newTurnFixture(t, hooks, agent.Config{ID: "test"}, textReplies(4))
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
	f := newTurnFixture(t, hooks, agent.Config{ID: "test"}, textReplies(4))
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

func TestTurnStoppingStopWinsOverContinue(t *testing.T) {
	t.Parallel()

	hooks := &stubTurnStopping{
		continueTexts: []string{"more"},
		forceStop:     true,
		stopReason:    "finished",
	}
	f := newTurnFixture(t, hooks, agent.Config{ID: "test"}, textReplies(4))
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

func TestTurnStoppingWithoutContinueStaysSingleSegment(t *testing.T) {
	t.Parallel()

	hooks := &stubTurnStopping{}
	f := newTurnFixture(t, hooks, agent.Config{ID: "test"}, textReplies(4))
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	events := f.sessionEvents(t)
	if got := countEvents(events, agentkit.EventTurnContinue); got != 0 {
		t.Fatalf("turn/continue events = %d, want 0", got)
	}
	if got := countEvents(events, agentkit.EventStepStart); got != 1 {
		t.Fatalf("step/start events = %d, want 1", got)
	}
}
