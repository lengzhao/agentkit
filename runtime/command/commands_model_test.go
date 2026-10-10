package command_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capllm "github.com/lengzhao/agentkit/cap/llm"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/command"
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

// stubSpawner feeds /model a fixed subagent definition list for validation tests.
type stubSpawner struct {
	defs []capsubagent.Definition
}

func (s *stubSpawner) Definitions(context.Context) ([]capsubagent.Definition, error) {
	return s.defs, nil
}
func (s *stubSpawner) Run(context.Context, capsubagent.Request) (capsubagent.Result, error) {
	return capsubagent.Result{}, fmt.Errorf("not implemented in stub")
}

// stubCatalogLLM lists a fixed model catalog for /model show/warning tests.
type stubCatalogLLM struct{ ids []string }

func (p *stubCatalogLLM) Name() string { return "stub" }
func (p *stubCatalogLLM) Stream(context.Context, agentkit.LLMRequest) (agentkit.LLMStream, error) {
	return nil, fmt.Errorf("not implemented in stub")
}
func (p *stubCatalogLLM) CatalogModels() []capllm.ModelEntry {
	out := make([]capllm.ModelEntry, 0, len(p.ids))
	for _, id := range p.ids {
		out = append(out, capllm.ModelEntry{ID: id})
	}
	return out
}

func newModelCommand(t *testing.T, subagents capsubagent.Spawner, llm agentkit.LLMProvider) (agentkit.Command, workspace.Service, context.Context) {
	t.Helper()
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "cli:test"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	loop := &stubCatalogLoop{
		agents: []agentkit.Agent{
			stubModelAgent{id: "assistant", model: "gpt-5.4"},
		},
		defaultAgent: "assistant",
	}
	ws := rtworkspace.Static(t.TempDir())
	provider, err := command.NewCatalogCommands(command.CatalogCommandsConfig{}, command.CatalogCommandsDeps{
		Loop:         loop,
		SessionStore: store,
		Workspace:    ws,
		Subagents:    subagents,
		LLM:          llm,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range provider.Commands() {
		if cmd.Name() == "model" {
			ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
				Conversation: "cli:test",
				Workspace:    "cli:test",
			})
			return cmd, ws, ctx
		}
	}
	t.Fatal("missing /model command")
	return nil, nil, nil
}

// /model -g sub <名> 写 global models["sub:<名>"]；名字对定义列表校验；legacy sub: 前缀兼容。
func TestModelCommandSubagentGlobal(t *testing.T) {
	t.Parallel()

	spawner := &stubSpawner{defs: []capsubagent.Definition{
		{Name: "researcher"},
		{Name: "vision", Model: "vision-model"},
	}}
	modelCmd, ws, ctx := newModelCommand(t, spawner, nil)

	// 1) 关键字形式 set：写 global models["sub:researcher"]。
	out, err := modelCmd.CommandExec(ctx, "-g sub researcher deepseek-v4-flash")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "subagent global model (researcher): deepseek-v4-flash") {
		t.Fatalf("set out = %q", out)
	}
	got, err := sessbind.GlobalModelBind(ctx, ws, agentkit.AgentID("sub:researcher"))
	if err != nil || got != "deepseek-v4-flash" {
		t.Fatalf("sub bind = %q err=%v", got, err)
	}
	// 主 agent 的 global 键不受影响。
	got, err = sessbind.GlobalModelBind(ctx, ws, agentkit.AgentID("assistant"))
	if err != nil || got != "" {
		t.Fatalf("assistant should have no global model: %q err=%v", got, err)
	}

	// 2) 拼错的子 agent 名被校验拦下，并列出可用名单。
	_, err = modelCmd.CommandExec(ctx, "-g sub researchr deepseek-v4-flash")
	if err == nil {
		t.Fatal("typo'd subagent name should fail")
	}
	if !strings.Contains(err.Error(), "available: researcher, vision") {
		t.Fatalf("err = %v, want available list", err)
	}

	// 3) show 单个：带 md model 提示。
	out, err = modelCmd.CommandExec(ctx, "-g sub vision")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "subagent vision global model: (not set)") ||
		!strings.Contains(out, "definition model (agents/vision.md): vision-model") {
		t.Fatalf("show out = %q", out)
	}

	// 4) legacy sub:<名> 前缀别名。
	out, err = modelCmd.CommandExec(ctx, "-g sub:researcher")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "subagent researcher global model: deepseek-v4-flash") {
		t.Fatalf("legacy show out = %q", out)
	}

	// 5) 不带 -g 时给出指引而非误写 session bind。
	if _, err := modelCmd.CommandExec(ctx, "sub researcher deepseek-v4-flash"); err == nil {
		t.Fatal("session-scoped subagent config should fail")
	} else if !strings.Contains(err.Error(), "/model -g sub") {
		t.Fatalf("err = %v, want usage hint", err)
	}

	// 6) reset 清空子键。
	if _, err := modelCmd.CommandExec(ctx, "-g sub researcher reset"); err != nil {
		t.Fatal(err)
	}
	got, err = sessbind.GlobalModelBind(ctx, ws, agentkit.AgentID("sub:researcher"))
	if err != nil || got != "" {
		t.Fatalf("after reset bind = %q err=%v", got, err)
	}
}

// /model -g sub * <model> 写 wildcard 键，作用于所有未单独配置/未在 md 定型的子 agent。
func TestModelCommandSubagentWildcard(t *testing.T) {
	t.Parallel()

	spawner := &stubSpawner{defs: []capsubagent.Definition{
		{Name: "researcher"},
		{Name: "vision", Model: "vision-model"},
	}}
	modelCmd, ws, ctx := newModelCommand(t, spawner, nil)

	out, err := modelCmd.CommandExec(ctx, "-g sub * deepseek-v4-flash")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "subagent global model (all subagents (*)): deepseek-v4-flash") {
		t.Fatalf("wildcard set out = %q", out)
	}
	got, err := sessbind.GlobalModelBind(ctx, ws, sessbind.SubagentModelWildcardKey)
	if err != nil || got != "deepseek-v4-flash" {
		t.Fatalf("wildcard bind = %q err=%v", got, err)
	}

	// overview：wildcard + 各定义（含 md 提示）一次看全。
	out, err = modelCmd.CommandExec(ctx, "-g sub")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "all subagents (*): deepseek-v4-flash") {
		t.Fatalf("overview missing wildcard: %q", out)
	}
	if !strings.Contains(out, "researcher: (not set)") {
		t.Fatalf("overview missing researcher: %q", out)
	}
	if !strings.Contains(out, "vision: (not set) (md model: vision-model takes precedence)") {
		t.Fatalf("overview missing vision md note: %q", out)
	}

	// reset wildcard。
	if _, err := modelCmd.CommandExec(ctx, "-g sub all reset"); err != nil {
		t.Fatal(err)
	}
	got, err = sessbind.GlobalModelBind(ctx, ws, sessbind.SubagentModelWildcardKey)
	if err != nil || got != "" {
		t.Fatalf("after reset wildcard = %q err=%v", got, err)
	}
}

// 有 LLM catalog 时：show 列出可用模型；set 未知模型名给非阻断警告。
func TestModelCommandCatalogListingAndWarning(t *testing.T) {
	t.Parallel()

	modelCmd, _, ctx := newModelCommand(t, nil, &stubCatalogLLM{ids: []string{"gpt-5.4", "deepseek-v4-flash"}})

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

func TestModelCommandSetAndShow(t *testing.T) {
	t.Parallel()

	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "cli:test"})
	if err != nil {
		t.Fatal(err)
	}
	store := sessstore.NewStaticStore(mem)
	loop := &stubCatalogLoop{
		agents: []agentkit.Agent{
			stubModelAgent{id: "assistant", model: "gpt-5.4"},
		},
		defaultAgent: "assistant",
	}
	ws := rtworkspace.Static(t.TempDir())
	provider, err := command.NewCatalogCommands(command.CatalogCommandsConfig{}, command.CatalogCommandsDeps{
		Loop:         loop,
		SessionStore: store,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	var modelCmd agentkit.Command
	for _, cmd := range provider.Commands() {
		if cmd.Name() == "model" {
			modelCmd = cmd
			break
		}
	}
	if modelCmd == nil {
		t.Fatal("missing /model command")
	}

	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: "cli:test",
		Workspace:    "cli:test",
	})
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
	loop := &stubCatalogLoop{
		agents: []agentkit.Agent{
			stubModelAgent{id: "assistant", model: "gpt-5.4"},
			stubModelAgent{id: "worker", model: "gpt-5.4"},
		},
		defaultAgent: "assistant",
	}
	ws := rtworkspace.Static(t.TempDir())
	provider, err := command.NewCatalogCommands(command.CatalogCommandsConfig{}, command.CatalogCommandsDeps{
		Loop:         loop,
		SessionStore: store,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	var agentCmd, modelCmd agentkit.Command
	for _, cmd := range provider.Commands() {
		switch cmd.Name() {
		case "agent":
			agentCmd = cmd
		case "model":
			modelCmd = cmd
		}
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: "cli:test",
		Workspace:    "cli:test",
	})

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

	modelCmd, _, ctx := newModelCommand(t, nil, nil)

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
