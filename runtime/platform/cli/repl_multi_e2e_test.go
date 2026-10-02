package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/command"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/runner"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/runtime/tools"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// E2E-100: CLI REPL serves three sequential user turns on the same delivery session.
func TestE2ECLIReplThreeSequentialTurns(t *testing.T) {
	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	sessionCmds, err := sessstore.NewCommands(sessstore.CommandsConfig{}, sessstore.CommandsDeps{SessionStore: store})
	if err != nil {
		t.Fatal(err)
	}
	commands, err := command.NewFromProviders(command.Config{}, []agentkit.CommandProvider{sessionCmds})
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
	plat.input = NewInput(strings.NewReader("turn one\nturn two\nturn three\n/exit\n"))

	llmProvider, err := llm.NewScripted(llm.ScriptedConfig{
		Steps: []llm.ScriptedStep{
			{Text: "reply-1"},
			{Text: "reply-2"},
			{Text: "reply-3"},
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
	ag, err := agent.New(agent.Config{ID: "coder"}, agent.Deps{
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
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := root.Run(ctx, nil); err != nil && err != context.Canceled {
		t.Fatalf("runner: %v", err)
	}

	events, err := loadAllSessionEvents(store, rctx.DefaultCLISessionID)
	if err != nil {
		t.Fatal(err)
	}
	if got := agenttest.CountEvents(events, agentkit.EventTurnEnd); got != 3 {
		t.Fatalf("turn/end = %d, want 3", got)
	}
	if !userMessageContains(events, "turn one") || !userMessageContains(events, "turn two") || !userMessageContains(events, "turn three") {
		t.Fatal("session missing one of the three user turns")
	}
}
