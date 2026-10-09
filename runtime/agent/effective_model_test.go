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

func TestEffectiveModelFollowsActiveSessionMapping(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	ws := rtworkspace.Static(dir)
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "sess"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	const entry = agentkit.SessionID("feishu:oc_test:entry")
	const active = agentkit.SessionID("feishu:oc_test:new:20260101")
	if err := store.SetActiveSession(ctx, entry, active); err != nil {
		t.Fatal(err)
	}
	if err := store.SetModelBind(ctx, active, "session-model"); err != nil {
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

	ctx = rctx.WithConversation(ctx, string(entry))
	if got := ag.EffectiveModel(ctx); got != "session-model" {
		t.Fatalf("EffectiveModel = %q, want session-model (bind on active session)", got)
	}
}

// 子 agent（ctx 带 SubagentModelScope）：md 的 model 是契约，盖过显式 global 配置。
func TestEffectiveModelSubagentDefinitionBeatsGlobalBinds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws := rtworkspace.Static(t.TempDir())
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "sess"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	if err := sessbind.SetGlobalModelBind(ctx, ws, "sub:researcher", "child-global-model"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalModelBind(ctx, ws, "assistant", "parent-global-model"); err != nil {
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
	rt, err := agent.New(agent.Config{ID: "sub:researcher", Model: "from-md"}, agent.Deps{
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

	scoped := rctx.WithSubagentModelScope(ctx, rctx.SubagentModelScope{
		ParentAgentID: "assistant",
	})
	if got := ag.EffectiveModel(scoped); got != "from-md" {
		t.Fatalf("EffectiveModel = %q, want from-md (definition beats binds)", got)
	}
}

// 子 agent md 未写 model 时：global[sub:名] > global[sub:*]（wildcard）> global[父agent]。
// 会话级 /model 不参与：父会话 bind 对子 agent 无效。
func TestEffectiveModelSubagentGlobalOrderWithoutDefinition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws := rtworkspace.Static(t.TempDir())
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "sess"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	const parent = agentkit.SessionID("cli:default")

	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	toolRuntime, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := agent.New(agent.Config{ID: "sub:researcher"}, agent.Deps{
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

	scoped := rctx.WithSubagentModelScope(ctx, rctx.SubagentModelScope{
		ParentAgentID: "assistant",
	})

	// 0) 父会话 bind 不参与子 agent 解析。
	if err := store.SetModelBind(ctx, parent, "parent-session-model"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalModelBind(ctx, ws, "sub:researcher", "child-global-model"); err != nil {
		t.Fatal(err)
	}
	if got := ag.EffectiveModel(scoped); got != "child-global-model" {
		t.Fatalf("EffectiveModel = %q, want child-global-model (parent session bind must not apply)", got)
	}

	// 1) 子 agent 专属 global（/model -g sub researcher）优先，压过 wildcard 与父键。
	if err := sessbind.SetGlobalModelBind(ctx, ws, sessbind.SubagentModelWildcardKey, "wildcard-model"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalModelBind(ctx, ws, "assistant", "parent-global-model"); err != nil {
		t.Fatal(err)
	}
	if got := ag.EffectiveModel(scoped); got != "child-global-model" {
		t.Fatalf("EffectiveModel = %q, want child-global-model", got)
	}

	// 2) 子键清空后先用 wildcard（/model -g sub *）。
	if err := sessbind.SetGlobalModelBind(ctx, ws, "sub:researcher", ""); err != nil {
		t.Fatal(err)
	}
	if got := ag.EffectiveModel(scoped); got != "wildcard-model" {
		t.Fatalf("EffectiveModel = %q, want wildcard-model", got)
	}

	// 3) wildcard 清空后落到父 agent global。
	if err := sessbind.SetGlobalModelBind(ctx, ws, sessbind.SubagentModelWildcardKey, ""); err != nil {
		t.Fatal(err)
	}
	if got := ag.EffectiveModel(scoped); got != "parent-global-model" {
		t.Fatalf("EffectiveModel = %q, want parent-global-model", got)
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
