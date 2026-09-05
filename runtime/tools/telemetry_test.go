package tools_test

import (
	"context"
	"encoding/json"
	"testing"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/agentkit/runtime/tools")

type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "echo" }
func (echoTool) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{Type: "object"}
}
func (echoTool) Call(_ context.Context, raw json.RawMessage) (string, error) {
	return string(raw), nil
}

func TestExecuteRecordsToolObservation(t *testing.T) {
	t.Parallel()

	rec := &telemetry.RecordingExporter{}
	rt, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.Tool{echoTool{}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := telemetry.WithExporter(context.Background(), rec)
	ctx = session.ApplyEnvelopeToContext(ctx, agentkit.TurnEnvelope{Conversation: "cli:default", Workspace: "cli:default"})
	ctx = session.WithAgentID(ctx, agentkit.AgentID("coder"))

	result, err := rt.Execute(ctx, agentkit.ToolCall{
		ID:    "call-1",
		Name:  "echo",
		Input: []byte(`{"msg":"hi"}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Content == "" {
		t.Fatal("expected tool result")
	}

	_, observations, _ := rec.Snapshot()
	if len(observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(observations))
	}
	if observations[0].Meta.Kind != captelemetry.KindTool {
		t.Fatalf("kind = %q", observations[0].Meta.Kind)
	}
}
