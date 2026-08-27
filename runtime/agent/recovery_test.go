package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
)

// recordingLLM captures the history it is asked to complete, which is how these
// tests assert what a resumed turn actually sends to the provider.
type recordingLLM struct {
	inner    agentkit.LLMProvider
	requests [][]agentkit.ModelMessage
}

func (p *recordingLLM) Name() string { return "recording" }

func (p *recordingLLM) Stream(ctx context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	p.requests = append(p.requests, req.Messages)
	return p.inner.Stream(ctx, req)
}

// crashDir writes a session file that ends mid-tool-call, as a killed process
// would leave it, and returns the store that reopens it.
func crashedStore(t *testing.T) (agentkit.SessionStore, agentkit.SessionID) {
	t.Helper()
	dir := t.TempDir()
	sessionID := agentkit.SessionID("test:crashresume")
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendTurnStart(ctx, sess, "test"); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, "test", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "read the file"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendStepStart(ctx, sess, "test", 0); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, "test", agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:      "assistant",
		ToolCalls: []agentkit.ToolCall{{ID: "crashed-call", Name: "read", Input: []byte(`{"path":"README.md"}`)}},
	}); err != nil {
		t.Fatal(err)
	}
	return store, sessionID
}

func newAgentOn(t *testing.T, store agentkit.SessionStore, provider agentkit.LLMProvider) agentkit.Agent {
	t.Helper()
	readPack, err := fs.NewFSMemory(fs.FSMemoryConfig{
		Files: map[string]string{"README.md": "hello"},
		Tools: []string{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{Tools: []agentkit.ToolPack{readPack}})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "test", MaxSteps: 3}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRT,
		Prompt:       assembler,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ag
}

func TestRunTurnRecoversCrashedSession(t *testing.T) {
	t.Parallel()

	store, sessionID := crashedStore(t)
	scripted, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{{Text: "picked up where I left off"}}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingLLM{inner: scripted}
	ag := newAgentOn(t, store, recorder)

	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, sessionID)
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "continue"}},
		},
	}); err != nil {
		t.Fatalf("run turn on a crashed session: %v", err)
	}

	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}

	if got := countEvents(events, agentkit.EventSessionRecovery); got != 1 {
		t.Fatalf("session/recovery events = %d, want 1", got)
	}
	// Two turn/end events: the repaired one and this turn's own.
	if got := countEvents(events, agentkit.EventTurnEnd); got != 2 {
		t.Fatalf("turn/end events = %d, want 2", got)
	}
	if got := session.ScanIncomplete(events); got != nil {
		t.Fatalf("session still reports an open turn: %+v", got)
	}

	// The prompt actually sent must answer the interrupted call.
	if len(recorder.requests) != 1 {
		t.Fatalf("llm requests = %d, want 1", len(recorder.requests))
	}
	sent := recorder.requests[0]
	if !containsInterruptedResult(sent, "crashed-call") {
		t.Fatalf("the interrupted call was not answered in the request:\n%+v", sent)
	}
}

func TestRunTurnLeavesCleanSessionAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: workspace.Static(dir)})
	if err != nil {
		t.Fatal(err)
	}
	scripted, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{{Text: "done"}}})
	if err != nil {
		t.Fatal(err)
	}
	ag := newAgentOn(t, store, scripted)

	sessionID := agentkit.SessionID("test:clean")
	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, sessionID)
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if got := countEvents(events, agentkit.EventSessionRecovery); got != 0 {
		t.Fatalf("session/recovery events = %d, want 0 on a fresh session", got)
	}
}

func containsInterruptedResult(messages []agentkit.ModelMessage, id agentkit.ToolCallID) bool {
	for _, msg := range messages {
		for _, result := range msg.ToolResults {
			if result.ID != id {
				continue
			}
			if strings.Contains(result.Content, "interrupted") {
				return true
			}
		}
	}
	return false
}
