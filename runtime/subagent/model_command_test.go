package subagent_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	"github.com/lengzhao/agentkit/runtime/subagent"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// stubSpawner feeds /submodel a fixed definition list for validation tests.
type stubSpawner struct {
	defs []capsubagent.Definition
}

func (s *stubSpawner) Definitions(context.Context) ([]capsubagent.Definition, error) {
	return s.defs, nil
}

func (s *stubSpawner) Run(context.Context, capsubagent.Request) (capsubagent.Result, error) {
	return capsubagent.Result{}, fmt.Errorf("not implemented in stub")
}

func newSubmodelCommand(t *testing.T, defs []capsubagent.Definition) (agentkit.Command, context.Context) {
	t.Helper()
	ws := rtworkspace.Static(t.TempDir())
	cmd := subagent.ModelCommand(ws, &stubSpawner{defs: defs})
	if cmd.Name() != "submodel" {
		t.Fatalf("name = %q", cmd.Name())
	}
	return cmd, context.Background()
}

func testDefs() []capsubagent.Definition {
	return []capsubagent.Definition{
		{Name: "researcher"},
		{Name: "vision", Model: "vision-model"},
	}
}

// /submodel <名> <model> 写 global models["sub:<名>"]；名字对定义列表校验；legacy sub: 前缀兼容。
func TestSubmodelCommandSetShowReset(t *testing.T) {
	t.Parallel()

	cmd, ctx := newSubmodelCommand(t, testDefs())

	// 1) set：写 global models["sub:researcher"]。
	out, err := cmd.CommandExec(ctx, "researcher deepseek-v4-flash")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "subagent global model (researcher): deepseek-v4-flash") {
		t.Fatalf("set out = %q", out)
	}

	// 2) 拼错的子 agent 名被校验拦下，并列出可用名单。
	_, err = cmd.CommandExec(ctx, "researchr deepseek-v4-flash")
	if err == nil {
		t.Fatal("typo'd subagent name should fail")
	}
	if !strings.Contains(err.Error(), "available: researcher, vision") {
		t.Fatalf("err = %v, want available list", err)
	}

	// 3) show 单个：带 md model 提示。
	out, err = cmd.CommandExec(ctx, "vision")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "subagent vision global model: (not set)") ||
		!strings.Contains(out, "definition model (agents/vision.md): vision-model") {
		t.Fatalf("show out = %q", out)
	}

	// 4) legacy sub:<名> 前缀别名。
	out, err = cmd.CommandExec(ctx, "sub:researcher")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "subagent researcher global model: deepseek-v4-flash") {
		t.Fatalf("legacy show out = %q", out)
	}

	// 5) 肌肉记忆里的 -g 被容忍（绑定本来就是全局）。
	if _, err := cmd.CommandExec(ctx, "-g researcher"); err != nil {
		t.Fatalf("-g tolerated: %v", err)
	}

	// 6) reset 清空子键。
	if _, err := cmd.CommandExec(ctx, "researcher reset"); err != nil {
		t.Fatal(err)
	}
}

// /submodel * <model> 写 wildcard 键；总览一次看全 wildcard、各定义与 md 提示。
func TestSubmodelCommandWildcardAndOverview(t *testing.T) {
	t.Parallel()

	cmd, ctx := newSubmodelCommand(t, testDefs())

	out, err := cmd.CommandExec(ctx, "* deepseek-v4-flash")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "subagent global model (all subagents (*)): deepseek-v4-flash") {
		t.Fatalf("wildcard set out = %q", out)
	}

	out, err = cmd.CommandExec(ctx, "")
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

	if _, err := cmd.CommandExec(ctx, "all reset"); err != nil {
		t.Fatal(err)
	}
}

// 写入的键与 sessbind 解析契约一致：子键、wildcard 各归其位。
func TestSubmodelCommandBindKeys(t *testing.T) {
	t.Parallel()

	ws := rtworkspace.Static(t.TempDir())
	defs := testDefs()
	cmd := subagent.ModelCommand(ws, &stubSpawner{defs: defs})
	ctx := context.Background()

	if _, err := cmd.CommandExec(ctx, "researcher deepseek-v4-flash"); err != nil {
		t.Fatal(err)
	}
	if _, err := cmd.CommandExec(ctx, "* gpt-5.4-mini"); err != nil {
		t.Fatal(err)
	}

	got, err := sessbind.GlobalModelBind(ctx, ws, agentkit.AgentID("sub:researcher"))
	if err != nil || got != "deepseek-v4-flash" {
		t.Fatalf("sub bind = %q err=%v", got, err)
	}
	got, err = sessbind.GlobalModelBind(ctx, ws, sessbind.SubagentModelWildcardKey)
	if err != nil || got != "gpt-5.4-mini" {
		t.Fatalf("wildcard bind = %q err=%v", got, err)
	}
	// 主 agent 键不受影响。
	got, err = sessbind.GlobalModelBind(ctx, ws, agentkit.AgentID("assistant"))
	if err != nil || got != "" {
		t.Fatalf("assistant should have no global model: %q err=%v", got, err)
	}
}

// Spawner 经 CommandProvider 同时贡献 /subagent 与 /submodel。
func TestSpawnerContributesCommands(t *testing.T) {
	t.Parallel()

	spawner := &subagent.Spawner{}
	for _, cmd := range spawner.Commands() {
		if cmd.Name() == "submodel" {
			return
		}
	}
	t.Fatal("Spawner.Commands() missing /submodel")
}
