package agentkit

import (
	"context"
	"encoding/json"
	"testing"
)

type toolBuilderTestInput struct {
	Name string `json:"name"`
}

func TestNewToolFormatsStringOutputAsText(t *testing.T) {
	tool, err := NewTool("greet", func(_ context.Context, in toolBuilderTestInput) (string, error) {
		return "hello " + in.Name, nil
	}).Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	out, err := tool.Call(context.Background(), json.RawMessage(`{"name":"agent"}`))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if out != "hello agent" {
		t.Fatalf("Call() = %q, want hello agent", out)
	}
}

func TestNewToolFormatsStructOutputAsJSON(t *testing.T) {
	type showOutput struct {
		Status string `json:"status"`
	}
	tool, err := NewTool("show", func(_ context.Context, _ toolBuilderTestInput) (showOutput, error) {
		return showOutput{Status: "done"}, nil
	}).Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	out, err := tool.Call(context.Background(), json.RawMessage(`{"name":"agent"}`))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if out != `{"status":"done"}` {
		t.Fatalf("Call() = %q, want JSON status done", out)
	}
}
