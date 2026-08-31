package runner

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
)

func TestResolveRunnerSessionStoreFromAgentWhenUnset(t *testing.T) {
	t.Parallel()

	mem, err := session.NewMemory(session.MemoryConfig{ID: "cli:test"})
	if err != nil {
		t.Fatal(err)
	}
	store := session.NewStaticStore(mem)

	provider, err := llm.NewScripted(llm.ScriptedConfig{
		Steps: []llm.ScriptedStep{{Text: "ok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "assistant"}, agent.Deps{
		SessionStore: store,
		LLM:          provider,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    workspace.Static(t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}
	loopInst, err := loop.New(loop.Config{}, loop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}

	got := resolveRunnerSessionStore(Deps{Loop: loopInst})
	if got != store {
		t.Fatalf("session store = %T, want inherited static store", got)
	}
}
