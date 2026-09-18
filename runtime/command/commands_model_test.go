package command_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
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
