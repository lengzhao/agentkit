package loop_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capllm "github.com/lengzhao/agentkit/cap/llm"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtloop "github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type stubModelAgent struct {
	id    agentkit.AgentID
	model string
}

func (a stubModelAgent) ID() agentkit.AgentID                              { return a.id }
func (a stubModelAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }
func (a stubModelAgent) ConfiguredModel() string                           { return a.model }

// stubCatalog lists a fixed model catalog for /model show/warning tests.
type stubCatalog struct{ ids []string }

func (p *stubCatalog) CatalogModels() []capllm.ModelEntry {
	out := make([]capllm.ModelEntry, 0, len(p.ids))
	for _, id := range p.ids {
		out = append(out, capllm.ModelEntry{ID: id})
	}
	return out
}

func stubModelAgents() []agentkit.Agent {
	return []agentkit.Agent{stubModelAgent{id: "assistant", model: "gpt-5.4"}}
}

func modelTestContext() context.Context {
	return rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: "cli:test",
		Workspace:    "cli:test",
	})
}

func newModelCommand(t *testing.T, catalog capllm.ModelCatalog) (agentkit.Command, workspace.Service, context.Context) {
	t.Helper()
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "cli:test"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	ws := rtworkspace.Static(t.TempDir())
	cmd := rtloop.ModelCommand(stubModelAgents(), store, "assistant", ws, catalog)
	return cmd, ws, modelTestContext()
}

// 有 model catalog 时：show 列出可用模型；set 未知模型名给非阻断警告。
func TestModelCommandCatalogListingAndWarning(t *testing.T) {
	t.Parallel()

	modelCmd, _, ctx := newModelCommand(t, &stubCatalog{ids: []string{"gpt-5.4", "deepseek-v4-flash"}})

	out, err := modelCmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "available: gpt-5.4, deepseek-v4-flash") {
		t.Fatalf("show missing catalog: %q", out)
	}

	out, err = modelCmd.CommandExec(ctx, "gpt5.4") // 拼错：少了连字符
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `warning: "gpt5.4" is not in the model catalog`) {
		t.Fatalf("set out = %q, want unknown-model warning", out)
	}

	out, err = modelCmd.CommandExec(ctx, "gpt-5.4")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "warning") {
		t.Fatalf("known model should not warn: %q", out)
	}
}

// 迁移期的旧语法（/model fallback、/model -g sub）必须响亮失败并指向新命令，
// 而不是被当成模型名写入 bind。
func TestModelCommandRelocatedScopes(t *testing.T) {
	t.Parallel()

	modelCmd, ws, ctx := newModelCommand(t, nil)

	for _, args := range []string{"fallback", "fb", "-g fallback"} {
		if _, err := modelCmd.CommandExec(ctx, args); err == nil {
			t.Fatalf("%q should fail", args)
		} else if !strings.Contains(err.Error(), "/fallback") {
			t.Fatalf("%q err = %v, want /fallback hint", args, err)
		}
	}
	for _, args := range []string{"sub", "sub researcher deepseek-v4-flash", "-g sub * x", "sub:researcher"} {
		if _, err := modelCmd.CommandExec(ctx, args); err == nil {
			t.Fatalf("%q should fail", args)
		} else if !strings.Contains(err.Error(), "/submodel") {
			t.Fatalf("%q err = %v, want /submodel hint", args, err)
		}
	}

	// 旧语法不得写入任何 bind。
	binds, err := sessbind.GlobalModelBinds(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(binds) != 0 {
		t.Fatalf("no binds expected, got %v", binds)
	}
}

func TestModelCommandSetAndShow(t *testing.T) {
	t.Parallel()

	modelCmd, _, ctx := newModelCommand(t, nil)

	out, err := modelCmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gpt-5.4") {
		t.Fatalf("show = %q", out)
	}

	out, err = modelCmd.CommandExec(ctx, "my-custom-model")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "my-custom-model") {
		t.Fatalf("set out = %q", out)
	}

	out, err = modelCmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "my-custom-model") {
		t.Fatalf("show after set = %q", out)
	}

	out, err = modelCmd.CommandExec(ctx, "reset")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gpt-5.4") {
		t.Fatalf("reset out = %q", out)
	}

	out, err = modelCmd.CommandExec(ctx, "-g shared-model")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "shared-model") {
		t.Fatalf("global set out = %q", out)
	}

	out, err = modelCmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "global override: shared-model") {
		t.Fatalf("show global = %q", out)
	}

	out, err = modelCmd.CommandExec(ctx, "-g reset")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "global model reset") {
		t.Fatalf("global reset out = %q", out)
	}
}

func TestModelGlobalUsesRoutedAgent(t *testing.T) {
	t.Parallel()

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "cli:test"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	agents := []agentkit.Agent{
		stubModelAgent{id: "assistant", model: "gpt-5.4"},
		stubModelAgent{id: "worker", model: "gpt-5.4"},
	}
	ws := rtworkspace.Static(t.TempDir())
	agentCmd := rtloop.AgentCommand(agents, store, "assistant", ws)
	modelCmd := rtloop.ModelCommand(agents, store, "assistant", ws, nil)
	ctx := modelTestContext()

	if _, err := agentCmd.CommandExec(ctx, "-g use worker"); err != nil {
		t.Fatal(err)
	}
	out, err := modelCmd.CommandExec(ctx, "-g worker-model")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "global model (worker): worker-model") {
		t.Fatalf("set out = %q", out)
	}

	got, err := sessbind.GlobalModelBind(ctx, ws, agentkit.AgentID("worker"))
	if err != nil || got != "worker-model" {
		t.Fatalf("worker global model = %q err=%v", got, err)
	}
	got, err = sessbind.GlobalModelBind(ctx, ws, agentkit.AgentID("assistant"))
	if err != nil || got != "" {
		t.Fatalf("assistant should have no global model: %q err=%v", got, err)
	}
}

// /model show 是查看状态，不是把模型名设为 "show"；有 session 覆盖时仍展示 global 绑定。
func TestModelCommandShowAliasAndGlobalWhileSessionOverride(t *testing.T) {
	t.Parallel()

	modelCmd, _, ctx := newModelCommand(t, nil)

	if _, err := modelCmd.CommandExec(ctx, "-g global-model"); err != nil {
		t.Fatal(err)
	}
	if _, err := modelCmd.CommandExec(ctx, "session-model"); err != nil {
		t.Fatal(err)
	}

	out, err := modelCmd.CommandExec(ctx, "show")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "session override: show") {
		t.Fatalf("/model show should not set model name: %q", out)
	}
	if !strings.Contains(out, "session override: session-model") ||
		!strings.Contains(out, "global override: global-model") ||
		!strings.Contains(out, "model: session-model") {
		t.Fatalf("show alias out = %q", out)
	}
}
