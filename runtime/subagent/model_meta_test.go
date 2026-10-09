package subagent

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type modelStubAgent struct {
	agentkit.AgentID
	model string
}

func (a modelStubAgent) ID() agentkit.AgentID                              { return a.AgentID }
func (a modelStubAgent) RunTurn(context.Context, agentkit.TurnInput) error { return nil }
func (a modelStubAgent) ConfiguredModel() string                           { return a.model }

func TestTelemetryModelPrefersDefinition(t *testing.T) {
	t.Parallel()
	def := capsubagent.Definition{Name: "r", Model: "from-def"}
	ag := modelStubAgent{AgentID: "cursor", model: "from-agent"}
	if got := telemetryModel(def, ag); got != "from-def" {
		t.Fatalf("model = %q, want from-def", got)
	}
}

func TestTelemetryModelFallsBackToAgent(t *testing.T) {
	t.Parallel()
	def := capsubagent.Definition{Name: "r"}
	ag := modelStubAgent{AgentID: "cursor", model: "from-agent"}
	if got := telemetryModel(def, ag); got != "from-agent" {
		t.Fatalf("model = %q, want from-agent", got)
	}
}

func TestInprocessTelemetryModelUsesDefinitionDefault(t *testing.T) {
	t.Parallel()
	def := capsubagent.Definition{Name: "researcher", Model: "from-md"}
	got := inprocessTelemetryModel(context.Background(), nil, "sub:researcher", def)
	if got != "from-md" {
		t.Fatalf("model = %q, want from-md", got)
	}
}

// md 的 model 是能力契约：显式 global 配置也不能盖住它。
func TestInprocessTelemetryModelDefinitionBeatsGlobalBinds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws := rtworkspace.Static(t.TempDir())
	if err := sessbind.SetGlobalModelBind(ctx, ws, "sub:researcher", "child-global-model"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalModelBind(ctx, ws, "assistant", "parent-global-model"); err != nil {
		t.Fatal(err)
	}

	scoped := rctx.WithSubagentModelScope(ctx, rctx.SubagentModelScope{
		ParentAgentID: "assistant",
	})
	def := capsubagent.Definition{Name: "researcher", Model: "from-md"}
	if got := inprocessTelemetryModel(scoped, ws, "sub:researcher", def); got != "from-md" {
		t.Fatalf("model = %q, want from-md (definition beats all binds)", got)
	}
}

// md 没写 model 时：global[sub:名] > global[sub:*]（wildcard）> global[父agent]。
func TestInprocessTelemetryModelGlobalOrderWithoutDefinition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ws := rtworkspace.Static(t.TempDir())
	scoped := rctx.WithSubagentModelScope(ctx, rctx.SubagentModelScope{
		ParentAgentID: "assistant",
	})
	def := capsubagent.Definition{Name: "researcher"}

	// 1) 子 agent 专属 global（/model -g sub researcher）优先，压过 wildcard。
	if err := sessbind.SetGlobalModelBind(ctx, ws, "sub:researcher", "child-global-model"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalModelBind(ctx, ws, sessbind.SubagentModelWildcardKey, "wildcard-model"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetGlobalModelBind(ctx, ws, "assistant", "parent-global-model"); err != nil {
		t.Fatal(err)
	}
	if got := inprocessTelemetryModel(scoped, ws, "sub:researcher", def); got != "child-global-model" {
		t.Fatalf("model = %q, want child-global-model", got)
	}

	// 2) 子键清空后先用 wildcard。
	if err := sessbind.SetGlobalModelBind(ctx, ws, "sub:researcher", ""); err != nil {
		t.Fatal(err)
	}
	if got := inprocessTelemetryModel(scoped, ws, "sub:researcher", def); got != "wildcard-model" {
		t.Fatalf("model = %q, want wildcard-model", got)
	}
	if got := inprocessTelemetryModel(scoped, ws, "sub:vision", def); got != "wildcard-model" {
		t.Fatalf("model = %q, want wildcard-model for other child", got)
	}

	// 3) wildcard 也清空后落到父 agent global。
	if err := sessbind.SetGlobalModelBind(ctx, ws, sessbind.SubagentModelWildcardKey, ""); err != nil {
		t.Fatal(err)
	}
	if got := inprocessTelemetryModel(scoped, ws, "sub:researcher", def); got != "parent-global-model" {
		t.Fatalf("model = %q, want parent-global-model", got)
	}
}
