package agent_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/runtime/tools"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestEffectiveModelSessionOverride(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "sess"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	const sid = agentkit.SessionID("cli:model-override")
	if err := store.SetModelBind(ctx, sid, "session-model"); err != nil {
		t.Fatal(err)
	}

	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := agent.New(agent.Config{ID: "assistant", Model: "default-model"}, agent.Deps{
		SessionStore: store,
		LLM:          &sizingLLM{},
		Tools:        toolRuntime,
		Prompt:       assembler,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	ag, ok := rt.(interface {
		EffectiveModel(context.Context) string
	})
	if !ok {
		t.Fatal("agent runtime missing EffectiveModel")
	}

	ctx = rctx.WithConversation(ctx, string(sid))
	if got := ag.EffectiveModel(ctx); got != "session-model" {
		t.Fatalf("EffectiveModel = %q, want session-model", got)
	}
}

func TestEffectiveModelGlobalOverride(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "sess"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	const sid = agentkit.SessionID("cli:global-model")
	if err := sessbind.SetGlobalModelBind(ctx, ws, "assistant", "global-model"); err != nil {
		t.Fatal(err)
	}

	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := agent.New(agent.Config{ID: "assistant", Model: "default-model"}, agent.Deps{
		SessionStore: store,
		LLM:          &sizingLLM{},
		Tools:        toolRuntime,
		Prompt:       assembler,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	ag := rt.(interface{ EffectiveModel(context.Context) string })

	ctx = rctx.WithConversation(ctx, string(sid))
	if got := ag.EffectiveModel(ctx); got != "global-model" {
		t.Fatalf("EffectiveModel = %q, want global-model", got)
	}
}
