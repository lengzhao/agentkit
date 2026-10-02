package memory

import (
	"context"
	"encoding/json"
	"testing"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type stubMemoryTool struct {
	last capmemory.MemoryToolInput
	out  capmemory.MemoryToolOutput
	err  error
}

func (s *stubMemoryTool) MemoryTool(_ context.Context, in capmemory.MemoryToolInput) (capmemory.MemoryToolOutput, error) {
	s.last = in
	if s.err != nil {
		return capmemory.MemoryToolOutput{}, s.err
	}
	if s.out.Message != "" || s.out.Success {
		return s.out, nil
	}
	return capmemory.MemoryToolOutput{Success: true, Message: "ok"}, nil
}

func TestNewMemoryToolRequiresDeps(t *testing.T) {
	_, err := NewMemoryTool(struct{}{}, MemoryToolDeps{})
	if err == nil {
		t.Fatal("expected error without memory dep")
	}
}

func TestMemoryToolDelegatesToService(t *testing.T) {
	stub := &stubMemoryTool{}
	tool, err := NewMemoryTool(struct{}{}, MemoryToolDeps{Memory: stub})
	if err != nil {
		t.Fatal(err)
	}
	raw := agenttest.CallTool(t, context.Background(), tool, `{"action":"add","content":"note"}`)
	var out capmemory.MemoryToolOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Success {
		t.Fatalf("output: %+v", out)
	}
	if stub.last.Action != "add" || stub.last.Content != "note" {
		t.Fatalf("delegated input: %+v", stub.last)
	}
	if tool.Name() != "memory" {
		t.Fatalf("name = %q", tool.Name())
	}
}
