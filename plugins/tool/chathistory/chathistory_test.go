package chathistory_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	caphistory "github.com/lengzhao/agentkit/cap/chathistory"
	"github.com/lengzhao/agentkit/plugins/tool/chathistory"
	"github.com/lengzhao/agentkit/runtime/session"
)

type stubProvider struct {
	lastReq caphistory.Request
	result  caphistory.Result
}

func (s *stubProvider) ReadChatHistory(_ context.Context, req caphistory.Request) (caphistory.Result, error) {
	s.lastReq = req
	return s.result, nil
}

type stubRouter struct {
	noopPlatform
	providers map[string]caphistory.Provider
}

func (r *stubRouter) ChatHistoryFor(id string) caphistory.Provider {
	return r.providers[id]
}

type noopPlatform struct{}

func (noopPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	return agentkit.MessageEvent{}, nil
}

func (noopPlatform) Send(context.Context, agentkit.OutboundEvent) error { return nil }

func TestChatHistoryReturnsEmptyWithoutProvider(t *testing.T) {
	t.Parallel()

	tool, err := chathistory.NewChatHistory(chathistory.ChatHistoryConfig{}, chathistory.ChatHistoryDeps{
		Platform: noopPlatform{},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := session.ContextWithDeliveryRoute(context.Background(), "feishu", agentkit.SessionID("feishu:oc_test"))
	ctx = func() context.Context { env := session.EnvelopeFromContext(ctx); env.Route = agentkit.SessionRoute("feishu", "delivery"); return session.ApplyEnvelopeToContext(ctx, env) }()

	raw, err := tool.Call(ctx, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Messages []caphistory.Message `json:"messages"`
		Count    int                  `json:"count"`
		Source   string               `json:"source,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 0 || out.Count != 0 {
		t.Fatalf("expected empty result, got %+v", out)
	}
}

func TestChatHistoryRoutesThroughMultiplex(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{
		result: caphistory.Result{
			Messages: []caphistory.Message{{ID: "om_1", Role: "user", Content: "hello"}},
			Source:   "feishu",
		},
	}
	router := &stubRouter{providers: map[string]caphistory.Provider{"feishu": provider}}

	tool, err := chathistory.NewChatHistory(chathistory.ChatHistoryConfig{}, chathistory.ChatHistoryDeps{Platform: router})
	if err != nil {
		t.Fatal(err)
	}

	ctx := session.ContextWithDeliveryRoute(context.Background(), "feishu", agentkit.SessionID("feishu:oc_test:t:om_root"))
	ctx = func() context.Context { env := session.EnvelopeFromContext(ctx); env.Route = agentkit.SessionRoute("feishu", "delivery"); return session.ApplyEnvelopeToContext(ctx, env) }()

	raw, err := tool.Call(ctx, []byte(`{"limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Messages []caphistory.Message `json:"messages"`
		Count    int                  `json:"count"`
		Source   string               `json:"source,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 1 || out.Source != "feishu" {
		t.Fatalf("unexpected output: %+v", out)
	}
	if !provider.lastReq.Thread {
		t.Fatal("expected thread=true by default")
	}
}

func TestChatHistoryThreadCanBeDisabled(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{result: caphistory.Result{Source: "feishu"}}
	router := &stubRouter{providers: map[string]caphistory.Provider{"feishu": provider}}

	tool, err := chathistory.NewChatHistory(chathistory.ChatHistoryConfig{}, chathistory.ChatHistoryDeps{Platform: router})
	if err != nil {
		t.Fatal(err)
	}

	ctx := session.ContextWithDeliveryRoute(context.Background(), "feishu", agentkit.SessionID("feishu:oc_test:t:om_root"))
	ctx = func() context.Context { env := session.EnvelopeFromContext(ctx); env.Route = agentkit.SessionRoute("feishu", "delivery"); return session.ApplyEnvelopeToContext(ctx, env) }()

	if _, err := tool.Call(ctx, []byte(`{"thread":false}`)); err != nil {
		t.Fatal(err)
	}
	if provider.lastReq.Thread {
		t.Fatal("expected thread=false when explicitly disabled")
	}
}
