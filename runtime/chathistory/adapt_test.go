package chathistory

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/chathistory"
)

type stubProvider struct{}

func (stubProvider) ReadChatHistory(context.Context, chathistory.Request) (chathistory.Result, error) {
	return chathistory.Result{Source: "stub"}, nil
}

type stubRouter struct {
	id string
}

func (r stubRouter) ChatHistoryFor(platformID string) chathistory.Provider {
	if platformID == r.id {
		return stubProvider{}
	}
	return nil
}

func TestRouterFromPlatformNil(t *testing.T) {
	if RouterFromPlatform(nil) != nil {
		t.Fatal("nil platform must yield nil router")
	}
}

func TestRouterFromPlatformRouter(t *testing.T) {
	inner := stubRouter{id: "chat-api"}
	got := RouterFromPlatform(inner)
	if got != inner {
		t.Fatal("existing Router must pass through")
	}
}

func TestRouterFromPlatformProvider(t *testing.T) {
	p := stubProvider{}
	r := RouterFromPlatform(p)
	if r == nil {
		t.Fatal("expected adapter router")
	}
	prov := r.ChatHistoryFor("any")
	if prov == nil {
		t.Fatal("adapter must return provider for any platform id")
	}
	res, err := prov.ReadChatHistory(context.Background(), chathistory.Request{
		SessionID: agentkit.SessionID("s1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != "stub" {
		t.Fatalf("got source %q", res.Source)
	}
}

func TestRouterFromPlatformUnknown(t *testing.T) {
	r := RouterFromPlatform(struct{}{})
	if r == nil {
		t.Fatal("expected noop router")
	}
	if p := r.ChatHistoryFor("x"); p != nil {
		t.Fatal("noop router must return nil provider")
	}
}
