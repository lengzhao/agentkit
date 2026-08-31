package agenttest

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
)

// EmptyToolsRuntime returns a tool runtime with no tools and allow-all approval.
func EmptyToolsRuntime(t *testing.T) agentkit.ToolRuntime {
	t.Helper()
	rt, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{Approval: AllowAll{}})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

// DefaultAssembler builds the standard prompt assembler used in smoke tests.
func DefaultAssembler(t *testing.T) agentkit.PromptAssembler {
	t.Helper()
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	return assembler
}

// ScriptedAgentConfig wires a minimal agent for smoke scenarios.
type ScriptedAgentConfig struct {
	AgentID  agentkit.AgentID
	MaxSteps int
	Steps    []llm.ScriptedStep
	Tools    agentkit.ToolRuntime
	Store    agentkit.SessionStore
}

// TestWorkspace returns a temp-dir workspace for agent tests.
func TestWorkspace(t *testing.T, root ...string) workspace.Service {
	t.Helper()
	if len(root) > 0 && root[0] != "" {
		return workspace.Static(root[0])
	}
	return workspace.Static(t.TempDir())
}

// NewScriptedAgent builds an agent on a temp store unless Store is set.
func NewScriptedAgent(t *testing.T, cfg ScriptedAgentConfig) (agentkit.Agent, agentkit.SessionStore) {
	t.Helper()
	if cfg.AgentID == "" {
		cfg.AgentID = "smoke"
	}
	if cfg.MaxSteps == 0 {
		cfg.MaxSteps = 5
	}
	store := cfg.Store
	var wsRoot string
	if store == nil {
		store, wsRoot = TempFileStore(t)
	}
	if cfg.Tools == nil {
		cfg.Tools = EmptyToolsRuntime(t)
	}
	ag, err := agent.New(agent.Config{ID: cfg.AgentID, MaxSteps: cfg.MaxSteps}, agent.Deps{
		SessionStore: store,
		LLM:          MustScripted(t, cfg.Steps...),
		Tools:        cfg.Tools,
		Prompt:       DefaultAssembler(t),
		Workspace:    TestWorkspace(t, wsRoot),
	})
	if err != nil {
		t.Fatal(err)
	}
	return ag, store
}

// SeedCrashedToolCall writes an incomplete turn ending on a pending tool call.
func SeedCrashedToolCall(t *testing.T, store agentkit.SessionStore, sessionID agentkit.SessionID, agentID agentkit.AgentID, call agentkit.ToolCall, userText string) agentkit.Session {
	t.Helper()
	ctx := context.Background()
	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendTurnStart(ctx, sess, agentID); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, agentID, agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: userText}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendStepStart(ctx, sess, agentID, 0); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, agentID, agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:      "assistant",
		ToolCalls: []agentkit.ToolCall{call},
	}); err != nil {
		t.Fatal(err)
	}
	return sess
}

// ToolsRuntime builds a tool runtime with optional policies and approval.
func ToolsRuntime(t *testing.T, deps tools.RuntimeDeps) agentkit.ToolRuntime {
	t.Helper()
	rt, err := tools.NewRuntime(tools.RuntimeConfig{}, deps)
	if err != nil {
		t.Fatal(err)
	}
	return rt
}
