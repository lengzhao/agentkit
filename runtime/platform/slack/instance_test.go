package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lengzhao/agentkit"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/platform/multiplex"
	"github.com/lengzhao/agentkit/runtime/rctx"
	slackapi "github.com/slack-go/slack"
)

type instanceCommands struct{ conversation string }

func (c *instanceCommands) List(context.Context) []agentkit.Command { return nil }
func (c *instanceCommands) Dispatch(ctx context.Context, _ string, _ string) (string, error) {
	c.conversation = rctx.ConversationFromContext(ctx)
	return "", nil
}
func TestNativeCommandAndMessageShareInstance(t *testing.T) {
	c := &instanceCommands{}
	p := &Platform{commands: c, inbox: common.NewInbox(2)}
	first := &Platform{}

	if _, err := multiplex.New(multiplex.Config{}, multiplex.Deps{Platforms: []agentkit.Platform{first, p}}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, text := range []string{"/new", "hello"} {
		p.enqueueInbound(ctx, delivery{sessionID: "slack:C1", channel: "C1"}, "U1", text, nil, nil, nil, false)
	}
	event, err := p.inbox.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	env := rctx.ResolveEnvelope(event, rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
	if c.conversation != "slack1:C1" || env.Conversation != c.conversation {
		t.Fatalf("command=%q normal=%q", c.conversation, env.Conversation)
	}
}

func TestSendToExplicitInstanceReachesUncachedSlackChannel(t *testing.T) {
	var channel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		channel = r.Form.Get("channel")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C2","ts":"1"}`))
	}))
	defer server.Close()
	p := &Platform{client: slackapi.New("token", slackapi.OptionAPIURL(server.URL+"/"))}
	p.outbound = common.NewOutbound(p.sendText, nil)
	mux, err := multiplex.New(multiplex.Config{}, multiplex.Deps{Platforms: []agentkit.Platform{&Platform{}, p}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, current := range []agentkit.RouteRef{{}, rctx.SessionRoute("slack", "slack:C1").WithInstance("slack1")} {
		route, err := rctx.ResolveRoute(rctx.ApplyEnvelopeToContext(ctx, agentkit.TurnEnvelope{Route: current}), capsdelivery.RouteInput{SessionID: "slack1:C2"})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}})
		if err := mux.Send(ctx, agentkit.OutboundEvent{Route: route, Type: agentkit.EventAssistantMessage, Data: data}); err != nil {
			t.Fatal(err)
		}
		if channel != "C2" {
			t.Fatalf("channel=%q", channel)
		}
	}
}
