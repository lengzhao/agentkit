package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// E2E-102: /agent use persists session bind (runtime.json) and routes later turns to that agent; derive is agent-scoped.
func TestE2ECLIAgentUseBindsAndRoutes(t *testing.T) {
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

	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{Approval: agenttest.AllowAll{}})
	if err != nil {
		t.Fatal(err)
	}

	coderLLM, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{{Text: "from-coder"}}})
	if err != nil {
		t.Fatal(err)
	}
	reviewerLLM, err := llm.NewScripted(llm.ScriptedConfig{Steps: []llm.ScriptedStep{{Text: "from-reviewer"}}})
	if err != nil {
		t.Fatal(err)
	}

	coder, err := agent.New(agent.Config{ID: "coder"}, agent.Deps{
		SessionStore: store,
		LLM:          coderLLM,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := agent.New(agent.Config{ID: "reviewer"}, agent.Deps{
		SessionStore: store,
		LLM:          reviewerLLM,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}

	loopInst, err := loop.New(loop.Config{DefaultAgent: "coder"}, loop.Deps{Agents: []agentkit.Agent{coder, reviewer}})
	if err != nil {
		t.Fatal(err)
	}
	loopCatalog, ok := loopInst.(agentkit.AgentCatalogLoop)
	if !ok {
		t.Fatal("loop must implement AgentCatalogLoop")
	}
	catalog, err := command.NewCatalogCommands(command.CatalogCommandsConfig{}, command.CatalogCommandsDeps{
		Loop:         loopCatalog,
		SessionStore: store,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	commands, err := command.NewFromProviders(command.Config{}, []agentkit.CommandProvider{
		sessionCmds,
		catalog,
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
	plat.input = NewInput(strings.NewReader("first turn\n/agent use reviewer\nsecond turn\n/exit\n"))

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

	sessionsDir, err := ws.Resolve(context.Background(), "sessions")
	if err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(sessionsDir, "cli_default", "runtime.json")
	raw, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatalf("runtime.json: %v", err)
	}
	if !strings.Contains(string(raw), "reviewer") {
		t.Fatalf("runtime.json = %s, want reviewer bind", raw)
	}

	bindCtx := context.Background()
	if got, err := store.(agentkit.SessionRuntimeStore).AgentBind(bindCtx, rctx.DefaultCLISessionID); err != nil || got != "reviewer" {
		t.Fatalf("AgentBind = %q, err = %v", got, err)
	}

	events, err := loadAllSessionEvents(store, rctx.DefaultCLISessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !assistantTextForAgent(events, "coder", "from-coder") {
		t.Fatal("expected coder assistant reply in session")
	}
	if !assistantTextForAgent(events, "reviewer", "from-reviewer") {
		t.Fatal("expected reviewer assistant reply after /agent use")
	}

	sess, err := store.Get(bindCtx, rctx.DefaultCLISessionID)
	if err != nil {
		t.Fatal(err)
	}
	reviewerCtx := rctx.ApplyEnvelopeToContext(bindCtx, agentkit.TurnEnvelope{
		Conversation: string(rctx.DefaultCLISessionID),
		AgentID:      agentkit.AgentID("reviewer"),
	})
	reviewerMsgs, err := sess.DeriveMessages(reviewerCtx)
	if err != nil {
		t.Fatal(err)
	}
	if !modelMessagesContainText(reviewerMsgs, "second turn") {
		t.Fatalf("reviewer derive missing second turn: %#v", reviewerMsgs)
	}
	if modelMessagesContainText(reviewerMsgs, "first turn") {
		t.Fatalf("reviewer derive must not include coder-scoped user text: %#v", reviewerMsgs)
	}

	coderCtx := rctx.ApplyEnvelopeToContext(bindCtx, agentkit.TurnEnvelope{
		Conversation: string(rctx.DefaultCLISessionID),
		AgentID:      agentkit.AgentID("coder"),
	})
	coderMsgs, err := sess.DeriveMessages(coderCtx)
	if err != nil {
		t.Fatal(err)
	}
	if !modelMessagesContainText(coderMsgs, "first turn") {
		t.Fatalf("coder derive missing first turn: %#v", coderMsgs)
	}
	if modelMessagesContainText(coderMsgs, "second turn") {
		t.Fatalf("coder derive must not include reviewer-scoped user text: %#v", coderMsgs)
	}
}

func assistantTextForAgent(events []agentkit.SessionEvent, agentID agentkit.AgentID, want string) bool {
	for _, ev := range events {
		if ev.Type != agentkit.EventAssistantMessage || ev.AgentID != agentID {
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

func modelMessagesContainText(msgs []agentkit.ModelMessage, want string) bool {
	for _, msg := range msgs {
		if strings.Contains(agenttest.ContentText(msg), want) {
			return true
		}
		for _, tr := range msg.ToolResults {
			if strings.Contains(tr.Content, want) {
				return true
			}
		}
	}
	return false
}
