package hooks

import (
	"context"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
)

type hookListProvider struct {
	hooks []agentkit.Hook
}

func (p hookListProvider) Hooks() []agentkit.Hook { return p.hooks }

func TestNewSkipsNilProviderAndHook(t *testing.T) {
	var n int
	rt, err := New(Config{}, Deps{Providers: []agentkit.HookProvider{
		nil,
		hookListProvider{hooks: []agentkit.Hook{
			nil,
			agentkit.OnBeforeStep(func(context.Context, *agentkit.BeforeStep) error {
				n++
				return nil
			}),
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.BeforeStep(context.Background(), &agentkit.BeforeStep{}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("beforeStep calls = %d", n)
	}
}

func TestBeforeStepChainOrderAndAbort(t *testing.T) {
	var order []string
	rt, err := New(Config{}, Deps{Providers: []agentkit.HookProvider{
		hookListProvider{hooks: []agentkit.Hook{
			agentkit.OnBeforeStep(func(context.Context, *agentkit.BeforeStep) error {
				order = append(order, "a")
				return nil
			}),
			agentkit.OnBeforeStep(func(context.Context, *agentkit.BeforeStep) error {
				order = append(order, "b")
				return errors.New("stop")
			}),
			agentkit.OnBeforeStep(func(context.Context, *agentkit.BeforeStep) error {
				order = append(order, "c")
				return nil
			}),
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = rt.BeforeStep(context.Background(), &agentkit.BeforeStep{})
	if err == nil {
		t.Fatal("expected error from second hook")
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Fatalf("order = %v", order)
	}
}

func TestHookTypesCollected(t *testing.T) {
	var beforeTool, afterTool, stopping, complete int
	multi := hookListProvider{hooks: []agentkit.Hook{
		agentkit.OnBeforeTool(func(context.Context, *agentkit.ToolCall) error {
			beforeTool++
			return nil
		}),
		agentkit.OnAfterTool(func(context.Context, *agentkit.ToolResult) error {
			afterTool++
			return nil
		}),
		agentkit.OnTurnStopping(func(context.Context, *agentkit.TurnStopping) error {
			stopping++
			return nil
		}),
		agentkit.OnTurnComplete(func(context.Context, *agentkit.TurnComplete) error {
			complete++
			return nil
		}),
	}}
	rt, err := New(Config{}, Deps{Providers: []agentkit.HookProvider{multi}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := rt.BeforeTool(ctx, &agentkit.ToolCall{}); err != nil {
		t.Fatal(err)
	}
	if err := rt.AfterTool(ctx, &agentkit.ToolResult{}); err != nil {
		t.Fatal(err)
	}
	if err := rt.TurnStopping(ctx, &agentkit.TurnStopping{}); err != nil {
		t.Fatal(err)
	}
	if err := rt.TurnComplete(ctx, &agentkit.TurnComplete{}); err != nil {
		t.Fatal(err)
	}
	if beforeTool != 1 || afterTool != 1 || stopping != 1 || complete != 1 {
		t.Fatalf("counts tool=%d after=%d stop=%d complete=%d", beforeTool, afterTool, stopping, complete)
	}
}
