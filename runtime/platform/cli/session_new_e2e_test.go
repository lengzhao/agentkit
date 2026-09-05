package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/command"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/runner"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// E2E-101: /new switches CLI logical session; prior history stays in the old file.
func TestE2ECLINewSwitchesSession(t *testing.T) {
	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	store, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	commands, err := command.NewFromProviders(command.Config{}, []agentkit.CommandProvider{
		store.(agentkit.CommandProvider),
	})
	if err != nil {
		t.Fatal(err)
	}

	platformInst, err := New(Config{}, Deps{
		Commands:     commands,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	plat := platformInst.(*Platform)
	plat.input = NewInput(strings.NewReader("first turn\n/new\nsecond turn\n/exit\n"))

	llmProvider, err := llm.NewScripted(llm.ScriptedConfig{
		Steps: []llm.ScriptedStep{
			{Text: "reply one"},
			{Text: "reply two"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{Approval: agenttest.AllowAll{}})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "coder", MaxSteps: 5}, agent.Deps{
		SessionStore: store,
		LLM:          llmProvider,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	loopInst, err := loop.New(loop.Config{DefaultAgent: "coder"}, loop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform:     platformInst,
		Loop:         loopInst,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := root.Run(ctx, nil); err != nil && err != context.Canceled {
		t.Fatalf("runner: %v", err)
	}

	current, err := store.(session.CLICurrentStore).ResolveCLICurrent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current == session.DefaultCLISessionID {
		t.Fatalf("cli current = %q, want a new session after /new", current)
	}

	firstEvents, err := loadAllSessionEvents(store, session.DefaultCLISessionID)
	if err != nil {
		t.Fatal(err)
	}
	secondEvents, err := loadAllSessionEvents(store, current)
	if err != nil {
		t.Fatal(err)
	}

	if got := agenttest.CountEvents(firstEvents, agentkit.EventUserMessage); got != 1 {
		t.Fatalf("first session user messages = %d, want 1", got)
	}
	if got := agenttest.CountEvents(secondEvents, agentkit.EventUserMessage); got != 1 {
		t.Fatalf("second session user messages = %d, want 1", got)
	}
	if !userMessageContains(firstEvents, "first turn") {
		t.Fatal("first session missing first turn message")
	}
	if userMessageContains(firstEvents, "second turn") {
		t.Fatal("first session must not contain second turn after /new")
	}
	if !userMessageContains(secondEvents, "second turn") {
		t.Fatal("second session missing second turn message")
	}
}

func loadAllSessionEvents(store agentkit.SessionStore, id agentkit.SessionID) ([]agentkit.SessionEvent, error) {
	sess, err := store.Get(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return session.ReadAllEvents(context.Background(), sess)
}

func userMessageContains(events []agentkit.SessionEvent, want string) bool {
	for _, ev := range events {
		if ev.Type != agentkit.EventUserMessage {
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			continue
		}
		if strings.Contains(agenttest.ContentText(msg), want) {
			return true
		}
	}
	return false
}
