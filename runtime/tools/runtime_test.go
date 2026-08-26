package tools_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/tools"
)

type stubTool struct {
	name string
	fn   func(context.Context, agentkit.ToolCall) (agentkit.ToolResult, error)
}

func (t stubTool) Name() string        { return t.name }
func (t stubTool) Description() string { return t.name }
func (t stubTool) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{Type: "object"}
}
func (t stubTool) Call(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	return t.fn(ctx, call)
}

type stubHooks struct {
	beforeTool func(context.Context, *agentkit.ToolCall) error
	afterTool  func(context.Context, *agentkit.ToolResult) error
}

func (h stubHooks) BeforeStep(context.Context, *agentkit.BeforeStep) error { return nil }
func (h stubHooks) BeforeTool(ctx context.Context, call *agentkit.ToolCall) error {
	if h.beforeTool != nil {
		return h.beforeTool(ctx, call)
	}
	return nil
}
func (h stubHooks) AfterTool(ctx context.Context, result *agentkit.ToolResult) error {
	if h.afterTool != nil {
		return h.afterTool(ctx, result)
	}
	return nil
}
func (h stubHooks) TurnStopping(context.Context, *agentkit.TurnStopping) error { return nil }

func TestRuntimeExecuteRunsBeforeAndAfterToolHooks(t *testing.T) {
	t.Parallel()

	var seenInput string
	rt, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.ToolPack{agentkit.Pack(stubTool{
			name: "demo",
			fn: func(_ context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
				seenInput = string(call.Input)
				return agentkit.ToolResult{
					ID:   call.ID,
					Name: call.Name,
					Content: []agentkit.ContentPart{{
						Type: "text",
						Text: "payload",
					}},
				}, nil
			},
		})},
		Hooks: stubHooks{
			beforeTool: func(_ context.Context, call *agentkit.ToolCall) error {
				call.Input = json.RawMessage(`{"mutated":true}`)
				return nil
			},
			afterTool: func(_ context.Context, result *agentkit.ToolResult) error {
				result.Content[0].Text = "trimmed"
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	result, err := rt.Execute(context.Background(), agentkit.ToolCall{
		ID:    "call-1",
		Name:  "demo",
		Input: json.RawMessage(`{"original":true}`),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if seenInput != `{"mutated":true}` {
		t.Fatalf("before-tool input = %q", seenInput)
	}
	if tools.ResultText(result) != "trimmed" {
		t.Fatalf("after-tool result = %q", tools.ResultText(result))
	}
}

func TestRuntimeExecuteTruncatesLargeResults(t *testing.T) {
	t.Parallel()

	rt, err := tools.NewRuntime(tools.RuntimeConfig{MaxResultBytes: 20}, tools.RuntimeDeps{
		Tools: []agentkit.ToolPack{agentkit.Pack(stubTool{
			name: "demo",
			fn: func(_ context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
				return agentkit.ToolResult{
					ID:   call.ID,
					Name: call.Name,
					Content: []agentkit.ContentPart{{
						Type: "text",
						Text: "01234567890123456789012345",
					}},
				}, nil
			},
		})},
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	result, err := rt.Execute(context.Background(), agentkit.ToolCall{ID: "call-1", Name: "demo"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	text := tools.ResultText(result)
	if len(text) <= 20 {
		t.Fatalf("expected truncated result, got %q", text)
	}
	if text[len(text)-len("\n...[truncated]"):] != "\n...[truncated]" {
		t.Fatalf("expected truncation suffix, got %q", text)
	}
}

func TestRuntimeExecuteEnforcesTimeout(t *testing.T) {
	t.Parallel()

	rt, err := tools.NewRuntime(tools.RuntimeConfig{
		DefaultTimeoutSeconds: 1,
		ToolTimeouts: map[string]int{
			"slow": 1,
		},
	}, tools.RuntimeDeps{
		Tools: []agentkit.ToolPack{agentkit.Pack(stubTool{
			name: "slow",
			fn: func(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
				select {
				case <-time.After(200 * time.Millisecond):
					return agentkit.ToolResult{
						ID:      call.ID,
						Name:    call.Name,
						Content: []agentkit.ContentPart{{Type: "text", Text: "done"}},
					}, nil
				case <-ctx.Done():
					return agentkit.ToolResult{}, ctx.Err()
				}
			},
		})},
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := rt.Execute(ctx, agentkit.ToolCall{ID: "call-1", Name: "slow"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if tools.ResultText(result) != "tool execution timed out" {
		t.Fatalf("unexpected timeout result: %q", tools.ResultText(result))
	}
}

func TestRuntimeExecuteDeniesBeforeHooks(t *testing.T) {
	t.Parallel()

	rt, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools: []agentkit.ToolPack{agentkit.Pack(stubTool{
			name: "demo",
			fn: func(_ context.Context, _ agentkit.ToolCall) (agentkit.ToolResult, error) {
				t.Fatal("tool body should not run")
				return agentkit.ToolResult{}, nil
			},
		})},
		Policies: []agentkit.Policy{agentkit.PolicyFunc(func(_ context.Context, _ agentkit.PolicyInput) agentkit.Decision {
			return agentkit.Deny("blocked")
		})},
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	result, err := rt.Execute(context.Background(), agentkit.ToolCall{ID: "call-1", Name: "demo"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if tools.ResultText(result) != "blocked" {
		t.Fatalf("unexpected deny result: %q", tools.ResultText(result))
	}
}

type stubProvider struct {
	tools []agentkit.Tool
}

func (p *stubProvider) ListTools(context.Context) ([]agentkit.Tool, error) {
	return p.tools, nil
}

func TestRuntimeVisibleIncludesDynamicTools(t *testing.T) {
	t.Parallel()

	dynamic := &stubProvider{tools: []agentkit.Tool{stubTool{
		name: "mcp__ping",
		fn: func(_ context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
			return agentkit.ToolResult{
				ID:      call.ID,
				Name:    call.Name,
				Content: []agentkit.ContentPart{{Type: "text", Text: "pong"}},
			}, nil
		},
	}}}
	rt, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{
		Tools:        []agentkit.ToolPack{agentkit.Pack(stubTool{name: "static"})},
		DynamicTools: []agentkit.ToolProvider{dynamic},
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	specs, err := rt.Visible(context.Background())
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	names := map[string]bool{}
	for _, spec := range specs {
		names[spec.Name] = true
	}
	if !names["static"] || !names["mcp__ping"] {
		t.Fatalf("visible tools = %v", names)
	}

	result, err := rt.Execute(context.Background(), agentkit.ToolCall{ID: "1", Name: "mcp__ping"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if tools.ResultText(result) != "pong" {
		t.Fatalf("result = %q", tools.ResultText(result))
	}
}
