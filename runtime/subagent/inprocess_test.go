package subagent

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/tool"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
)

type echoInput struct {
	Text string `json:"text"`
}

type echoOutput struct {
	Text string `json:"text"`
}

type fixture struct {
	spawner  subagent.Spawner
	store    agentkit.SessionStore
	parentID agentkit.SessionID
	ctx      context.Context
}

// newFixture wires a real child agent: real session store, real tool runtime,
// real agent loop. Only the model is scripted, so the assertions below are about
// what actually lands in the two sessions.
func newFixture(t *testing.T, defs map[string]string, steps []llm.ScriptedStep) fixture {
	t.Helper()

	agentsDir := t.TempDir()
	for name, body := range defs {
		writeDef(t, agentsDir, name, body)
	}
	ws := dirWorkspace{"local:agents": agentsDir}

	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{
		Workspace: workspace.Static(t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := llm.NewScripted(llm.ScriptedConfig{Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	finish, err := tool.NewFinish(tool.FinishConfig{}, tool.FinishDeps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	echo, err := agentkit.NewTool[echoInput, echoOutput]("echo", func(_ context.Context, in echoInput) (echoOutput, error) {
		return echoOutput(in), nil
	}).Description("echo text back").Build()
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.Tool{finish, echo},
	})
	if err != nil {
		t.Fatal(err)
	}
	spawner, err := New(Config{Dirs: []string{"local:agents"}, MaxSteps: 5}, Deps{
		Workspace:    ws,
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRuntime,
		Prompt:       &stubAssembler{},
	})
	if err != nil {
		t.Fatal(err)
	}

	parentID := agentkit.SessionID("cli:default")
	ctx := context.WithValue(context.Background(), agentkit.KeySessionID, parentID)
	ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentkit.AgentID("coding"))
	return fixture{spawner: spawner, store: store, parentID: parentID, ctx: ctx}
}

func (f fixture) events(t *testing.T, id agentkit.SessionID) []agentkit.SessionEvent {
	t.Helper()
	sess, err := f.store.Get(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	events, err := session.ReadAllEvents(f.ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

const researcherDef = `---
name: researcher
description: read-only research
tools: [finish]
---
You are the research subagent.
`

func TestRunTakesSummaryFromFinish(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"researcher.md": researcherDef}, []llm.ScriptedStep{
		{ToolCalls: []agentkit.ToolCall{llm.MustToolCall("finish", `{"status":"completed","summary":"loop keeps one turn per session"}`)}},
		{Text: "done"},
	})

	result, err := f.spawner.Run(f.ctx, subagent.Request{Agent: "Researcher", Task: "how does the loop serialize turns?"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != session.FinishCompleted {
		t.Errorf("status = %q, want %q", result.Status, session.FinishCompleted)
	}
	if result.Summary != "loop keeps one turn per session" {
		t.Errorf("summary = %q, want the finish summary", result.Summary)
	}
	if result.Agent != "researcher" {
		t.Errorf("agent = %q, want the canonical definition name", result.Agent)
	}
	if result.Steps != 2 {
		t.Errorf("steps = %d, want 2", result.Steps)
	}

	// The child's own steps must live in its own session, which is the whole
	// point of delegating.
	childEvents := f.events(t, agentkit.SessionID(result.Session))
	if len(childEvents) == 0 {
		t.Fatal("child session is empty")
	}
	if !strings.HasPrefix(result.Session, "sub:"+string(f.parentID)+":researcher:") {
		t.Errorf("child session id = %q, want it derived from the parent", result.Session)
	}

	parentEvents := f.events(t, f.parentID)
	types := make([]agentkit.EventType, 0, len(parentEvents))
	for _, ev := range parentEvents {
		types = append(types, ev.Type)
	}
	if len(types) != 2 || types[0] != agentkit.EventSubagentStart || types[1] != agentkit.EventSubagentEnd {
		t.Fatalf("parent events = %v, want subagent/start then subagent/end", types)
	}
	if !strings.Contains(string(parentEvents[1].Data), "loop keeps one turn per session") {
		t.Errorf("subagent/end data = %s, want the summary", parentEvents[1].Data)
	}
}

func TestRunFallsBackToLastAssistantText(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"researcher.md": researcherDef}, []llm.ScriptedStep{
		{Text: "the answer is 42"},
	})

	result, err := f.spawner.Run(f.ctx, subagent.Request{Agent: "researcher", Task: "answer"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != subagent.StatusStopped {
		t.Errorf("status = %q, want %q", result.Status, subagent.StatusStopped)
	}
	if result.Summary != "the answer is 42" {
		t.Errorf("summary = %q, want the last assistant text", result.Summary)
	}
}

func TestRunDeniesToolsOutsideDefinitionAllowlist(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"researcher.md": researcherDef}, []llm.ScriptedStep{
		{ToolCalls: []agentkit.ToolCall{llm.MustToolCall("echo", `{"text":"hello"}`)}},
		{Text: "could not use echo"},
	})

	result, err := f.spawner.Run(f.ctx, subagent.Request{Agent: "researcher", Task: "try to echo"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var denied bool
	for _, ev := range f.events(t, agentkit.SessionID(result.Session)) {
		if ev.Type == agentkit.EventToolResult && strings.Contains(string(ev.Data), "tool not available to this subagent") {
			denied = true
		}
	}
	if !denied {
		t.Fatal("expected the echo call to be denied: the definition only allows finish")
	}
}

func TestRunRejectsUnknownAgentWithAvailableNames(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"researcher.md": researcherDef}, []llm.ScriptedStep{{Text: "unused"}})

	_, err := f.spawner.Run(f.ctx, subagent.Request{Agent: "reviewer", Task: "review"})
	if err == nil {
		t.Fatal("expected an error for an unknown agent")
	}
	if !strings.Contains(err.Error(), "researcher") {
		t.Errorf("error = %q, want the available names so the model can retry", err)
	}
}

func TestRunRejectsNestedDelegation(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"researcher.md": researcherDef}, []llm.ScriptedStep{{Text: "unused"}})

	ctx := context.WithValue(f.ctx, keyInSubagent, true)
	if _, err := f.spawner.Run(ctx, subagent.Request{Agent: "researcher", Task: "delegate again"}); err == nil {
		t.Fatal("a subagent must not be able to delegate further")
	}
}

func TestRunRequiresParentSession(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"researcher.md": researcherDef}, []llm.ScriptedStep{{Text: "unused"}})

	if _, err := f.spawner.Run(context.Background(), subagent.Request{Agent: "researcher", Task: "go"}); err == nil {
		t.Fatal("expected an error without a parent session in context")
	}
}

func TestRunValidatesRequest(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{"researcher.md": researcherDef}, []llm.ScriptedStep{{Text: "unused"}})

	if _, err := f.spawner.Run(f.ctx, subagent.Request{Task: "no agent"}); err == nil {
		t.Error("expected an error for a missing agent name")
	}
	if _, err := f.spawner.Run(f.ctx, subagent.Request{Agent: "researcher", Task: "  "}); err == nil {
		t.Error("expected an error for an empty task")
	}
}

func TestDefinitionsListsCatalog(t *testing.T) {
	t.Parallel()

	f := newFixture(t, map[string]string{
		"researcher.md": researcherDef,
		"reviewer.md":   "---\ndescription: reviews code\n---\nYou review code.\n",
	}, []llm.ScriptedStep{{Text: "unused"}})

	defs, err := f.spawner.Definitions(f.ctx)
	if err != nil {
		t.Fatalf("definitions: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("got %d definitions, want 2", len(defs))
	}
	if defs[0].Name != "researcher" || defs[1].Name != "reviewer" {
		t.Errorf("names = %q, %q", defs[0].Name, defs[1].Name)
	}
}
