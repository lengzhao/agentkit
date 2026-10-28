package chathistory_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	caphistory "github.com/lengzhao/agentkit/cap/chathistory"
	"github.com/lengzhao/agentkit/plugins/tool/chathistory"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type stubProvider struct {
	lastReq caphistory.Request
	result  caphistory.Result
}

func (s *stubProvider) ReadChatHistory(_ context.Context, req caphistory.Request) (caphistory.Result, error) {
	s.lastReq = req
	return s.result, nil
}

type stubHistoryPlatform struct {
	providers map[string]caphistory.Provider
}

func (r *stubHistoryPlatform) ChatHistoryFor(route agentkit.RouteRef) caphistory.Provider {
	return r.providers[route.DestinationID()]
}

func (stubHistoryPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	return agentkit.MessageEvent{}, nil
}

func (stubHistoryPlatform) Send(context.Context, agentkit.OutboundEvent) error { return nil }

func TestChatHistoryReturnsEmptyWithoutProvider(t *testing.T) {
	t.Parallel()

	tool, err := chathistory.NewChatHistory(chathistory.ChatHistoryConfig{}, chathistory.ChatHistoryDeps{
		History: &stubHistoryPlatform{providers: map[string]caphistory.Provider{}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := rctx.ContextWithDeliveryRoute(context.Background(), "feishu", agentkit.SessionID("feishu:oc_test"))
	ctx = func() context.Context {
		env := rctx.EnvelopeFromContext(ctx)
		env.Route = rctx.SessionRoute("feishu", "delivery")
		return rctx.ApplyEnvelopeToContext(ctx, env)
	}()

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
	router := &stubHistoryPlatform{providers: map[string]caphistory.Provider{"feishu": provider}}

	tool, err := chathistory.NewChatHistory(chathistory.ChatHistoryConfig{}, chathistory.ChatHistoryDeps{History: router})
	if err != nil {
		t.Fatal(err)
	}

	ctx := rctx.ContextWithDeliveryRoute(context.Background(), "feishu", agentkit.SessionID("feishu:oc_test:t:om_root"))
	ctx = func() context.Context {
		env := rctx.EnvelopeFromContext(ctx)
		env.Route = rctx.SessionRoute("feishu", "delivery")
		return rctx.ApplyEnvelopeToContext(ctx, env)
	}()

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
	router := &stubHistoryPlatform{providers: map[string]caphistory.Provider{"feishu": provider}}

	tool, err := chathistory.NewChatHistory(chathistory.ChatHistoryConfig{}, chathistory.ChatHistoryDeps{History: router})
	if err != nil {
		t.Fatal(err)
	}

	ctx := rctx.ContextWithDeliveryRoute(context.Background(), "feishu", agentkit.SessionID("feishu:oc_test:t:om_root"))
	ctx = func() context.Context {
		env := rctx.EnvelopeFromContext(ctx)
		env.Route = rctx.SessionRoute("feishu", "delivery")
		return rctx.ApplyEnvelopeToContext(ctx, env)
	}()

	if _, err := tool.Call(ctx, []byte(`{"thread":false}`)); err != nil {
		t.Fatal(err)
	}
	if provider.lastReq.Thread {
		t.Fatal("expected thread=false when explicitly disabled")
	}
}

func TestChatHistoryUsesCurrentAdapterInstance(t *testing.T) {
	t.Parallel()
	first, second := &stubProvider{}, &stubProvider{}
	router := &stubHistoryPlatform{providers: map[string]caphistory.Provider{"feishu": first, "feishu1": second}}
	tool, err := chathistory.NewChatHistory(chathistory.ChatHistoryConfig{}, chathistory.ChatHistoryDeps{History: router})
	if err != nil {
		t.Fatal(err)
	}
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "feishu", Instance: "feishu1", ChannelID: "oc_1", ReplyTo: "old-message"})
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Route: route})
	if _, err := tool.Call(ctx, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if !first.lastReq.Route.IsZero() || second.lastReq.Route.DestinationID() != "feishu1" {
		t.Fatal("history read reached the wrong adapter")
	}
	if rctx.RouteReplyTo(second.lastReq.Route) != "" {
		t.Fatal("history route retained an ephemeral reply anchor")
	}
}
