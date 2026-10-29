package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/tools"
)

type scopedCatalogProvider struct{}

func (scopedCatalogProvider) ListTools(ctx context.Context) ([]agentkit.Tool, error) {
	owner := string(rctx.SessionIDFromContext(ctx))
	return []agentkit.Tool{stubTool{name: "scoped", fn: func(context.Context, json.RawMessage) (string, error) { return owner, nil }}}, nil
}
func TestDynamicCatalogIsExecutionScoped(t *testing.T) {
	rt, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{DynamicTools: []agentkit.ToolProvider{scopedCatalogProvider{}}})
	if err != nil {
		t.Fatal(err)
	}
	a := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "a"})
	b := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "b"})
	if _, err = rt.Visible(a); err != nil {
		t.Fatal(err)
	}
	if _, err = rt.Visible(b); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Execute(a, agentkit.ToolCall{Name: "scoped"})
	if err != nil || result.Content != "a" {
		t.Fatalf("another session's tool used: %+v %v", result, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprint(i)
			ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: id})
			if _, err := rt.Visible(ctx); err != nil {
				errs <- err
				return
			}
			out, err := rt.Execute(ctx, agentkit.ToolCall{Name: "scoped"})
			if err != nil || out.Content != id {
				errs <- fmt.Errorf("session %s got %+v: %v", id, out, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

type changingCatalogProvider struct{ calls int }

func (p *changingCatalogProvider) ListTools(context.Context) ([]agentkit.Tool, error) {
	p.calls++
	generation := p.calls
	return []agentkit.Tool{stubTool{name: "changing", fn: func(context.Context, json.RawMessage) (string, error) { return fmt.Sprint(generation), nil }}}, nil
}
func TestBatchExecutesPreflightCatalogSnapshot(t *testing.T) {
	provider := &changingCatalogProvider{}
	rt, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{DynamicTools: []agentkit.ToolProvider{provider}})
	if err != nil {
		t.Fatal(err)
	}
	batch := rt.(agentkit.ToolBatchRuntime)
	_, prepared, run, err := batch.PreflightTool(context.Background(), agentkit.ToolCall{ID: "prepared", Name: "changing"})
	if err != nil || !run {
		t.Fatalf("preflight: %v %v", run, err)
	}
	out, err := batch.RunToolBody(context.Background(), prepared)
	if err != nil || out.Content != "1" {
		t.Fatalf("executed a different catalog from preflight: %+v %v", out, err)
	}
}
