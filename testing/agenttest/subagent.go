package agenttest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/prompt"
	rtsubagent "github.com/lengzhao/agentkit/runtime/subagent"
	"github.com/lengzhao/agentkit/runtime/tools"
)

const DefaultResearcherDef = `---
name: researcher
description: read-only research for smoke
tools: [finish]
---
You are the research subagent for smoke tests.
`

// SubagentDelegateEnv wires a parent agent with a delegate tool and in-process subagent.
type SubagentDelegateEnv struct {
	Store     agentkit.SessionStore
	Agent     agentkit.Agent
	ParentID  agentkit.SessionID
	LogicalID agentkit.SessionID
	AgentsDir string
}

// SubagentDelegateConfig controls subagent smoke wiring.
type SubagentDelegateConfig struct {
	LogicalID     agentkit.SessionID
	ResearcherDef string
	Steps         []llm.ScriptedStep
	ParentAgentID agentkit.AgentID
	// NewFinishTool builds the child agent's finish tool from the session store,
	// e.g. an adapter of finish.NewFinish. Required: agenttest stays plugin-agnostic.
	NewFinishTool func(store agentkit.SessionStore) (agentkit.Tool, error)
	// NewDelegateTool builds the parent agent's delegate tool bound to the spawner,
	// e.g. an adapter of the tool/subagent plugin constructor. Required.
	NewDelegateTool func(spawner capsubagent.Spawner) (agentkit.Tool, error)
}

// NewSubagentDelegateEnv builds a scripted parent→child delegate stack on a temp store.
func NewSubagentDelegateEnv(t *testing.T, cfg SubagentDelegateConfig) SubagentDelegateEnv {
	t.Helper()

	if cfg.ResearcherDef == "" {
		cfg.ResearcherDef = DefaultResearcherDef
	}
	if len(cfg.Steps) == 0 {
		cfg.Steps = DefaultSubagentSmokeSteps()
	}
	if cfg.ParentAgentID == "" {
		cfg.ParentAgentID = "nex"
	}
	if cfg.NewFinishTool == nil || cfg.NewDelegateTool == nil {
		t.Fatal("SubagentDelegateConfig requires NewFinishTool and NewDelegateTool factories")
	}

	store, root := TempFileStore(t)
	agentsDir := filepath.Join(root, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "researcher.md"), []byte(cfg.ResearcherDef), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := MustScripted(t, cfg.Steps...)
	finishTool, err := cfg.NewFinishTool(store)
	if err != nil {
		t.Fatal(err)
	}
	childTools, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools:    []agentkit.Tool{finishTool},
		Approval: AllowAll{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}

	spawner, err := rtsubagent.New(rtsubagent.Config{Dirs: []string{"local:agents"}}, rtsubagent.Deps{
		Workspace:    DirWorkspace{"local:agents": agentsDir},
		SessionStore: store,
		LLM:          provider,
		Tools:        childTools,
		Prompt:       assembler,
	})
	if err != nil {
		t.Fatal(err)
	}

	delegateTool, err := cfg.NewDelegateTool(spawner)
	if err != nil {
		t.Fatal(err)
	}
	parentTools, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools:    []agentkit.Tool{delegateTool},
		Approval: AllowAll{},
	})
	if err != nil {
		t.Fatal(err)
	}

	parentID := agentkit.SessionID("chat-api:nex-channel")
	logicalID := cfg.LogicalID
	if logicalID == "" {
		logicalID = parentID
	}

	ag, err := agent.New(agent.Config{ID: cfg.ParentAgentID}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        parentTools,
		Prompt:       assembler,
		Workspace:    TestWorkspace(t, root),
	})
	if err != nil {
		t.Fatal(err)
	}

	return SubagentDelegateEnv{
		Store:     store,
		Agent:     ag,
		ParentID:  parentID,
		LogicalID: logicalID,
		AgentsDir: agentsDir,
	}
}

// DefaultSubagentSmokeSteps matches presets/subagent-smoke.yaml scripted order.
func DefaultSubagentSmokeSteps() []llm.ScriptedStep {
	return []llm.ScriptedStep{
		{
			Text: "这个问题交给 researcher。",
			ToolCalls: []agentkit.ToolCall{{
				ID:    "call-delegate",
				Name:  "delegate",
				Input: []byte(`{"agent":"researcher","task":"说明 loop 如何保证同一 session 串行"}`),
			}},
		},
		{
			ToolCalls: []agentkit.ToolCall{{
				ID:    "call-finish",
				Name:  "finish",
				Input: []byte(`{"status":"completed","summary":"loop 按 SessionID 取锁，同一 session 的 turn 串行执行"}`),
			}},
		},
		{Text: "子 turn 收尾。"},
		{Text: "researcher 结论：loop 按 SessionID 取锁，同一 session 的 turn 串行执行。"},
	}
}

// FindChildSessionID reads the child session id from subagent/start.
func FindChildSessionID(t *testing.T, events []agentkit.SessionEvent) agentkit.SessionID {
	t.Helper()
	for _, ev := range events {
		if ev.Type != agentkit.EventSubagentStart {
			continue
		}
		data := SubagentStart(t, ev)
		if data.Session == "" {
			t.Fatal("subagent/start missing child session id")
		}
		return agentkit.SessionID(data.Session)
	}
	t.Fatal("no subagent/start event")
	return ""
}

// AssertSubagentParentSession verifies post-delegate parent session invariants.
func AssertSubagentParentSession(t *testing.T, events []agentkit.SessionEvent) {
	t.Helper()
	if got := CountEvents(events, agentkit.EventSubagentStart); got != 1 {
		t.Fatalf("subagent/start = %d, want 1", got)
	}
	if got := CountEvents(events, agentkit.EventSubagentEnd); got != 1 {
		t.Fatalf("subagent/end = %d, want 1", got)
	}
	if got := CountEvents(events, agentkit.EventSessionRecovery); got != 0 {
		t.Fatalf("parent session/recovery = %d, want 0", got)
	}
	if got := CountEvents(events, agentkit.EventTurnEnd); got != 1 {
		t.Fatalf("parent turn/end = %d, want 1", got)
	}

	delegateResults := 0
	for _, ev := range events {
		if ev.Type != agentkit.EventToolResult {
			continue
		}
		result := ToolResult(t, ev)
		if result.ID != "call-delegate" {
			continue
		}
		delegateResults++
		if strings.Contains(result.Content, "interrupted") {
			t.Fatalf("delegate result must not be synthetic interrupted: %+v", result)
		}
		if !strings.Contains(result.Content, "串行执行") {
			t.Fatalf("delegate result = %q, want child summary", result.Content)
		}
	}
	if delegateResults != 1 {
		t.Fatalf("delegate tool/result = %d, want exactly one", delegateResults)
	}
}
