package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
	slackapi "github.com/slack-go/slack"
)

func businessSlackRoute() agentkit.RouteRef {
	return rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "slack", Instance: "business", ChannelID: "C123"})
}
func TestBusinessMessagesPublishReadEdit(t *testing.T) {
	var texts []string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		r.ParseForm()
		switch r.URL.Path {
		case "/chat.postMessage":
			calls++
			texts = append(texts, r.Form.Get("text"))
			fmt.Fprintf(w, `{"ok":true,"channel":"C123","ts":"100.%d"}`, calls)
		case "/chat.update":
			fmt.Fprint(w, `{"ok":true,"channel":"C123","ts":"100.1"}`)
		case "/conversations.history":
			fmt.Fprint(w, `{"ok":true,"messages":[{"type":"message","ts":"100.1","text":"restored"}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	p := &Platform{client: slackapi.New("token", slackapi.OptionAPIURL(srv.URL+"/"))}
	p.SetPlatformInstance("business")
	receipt, err := p.Publish(context.Background(), cap.PublishRequest{Route: businessSlackRoute(), Content: []cap.Part{{Type: "mention", UserID: "U123"}, {Type: "text", Text: strings.Repeat("x", 3100)}}})
	if err != nil || len(receipt.Messages) != 2 || !strings.HasPrefix(texts[0], "<@U123>") || receipt.Deduplication.Mode != "none" {
		t.Fatalf("%+v %v texts=%v", receipt, err, texts)
	}
	var ref cap.MessageRef
	raw, _ := json.Marshal(receipt.Messages[0])
	json.Unmarshal(raw, &ref)
	msg, err := p.ReadMessage(context.Background(), cap.ReadRequest{Message: ref})
	if err != nil || msg.Content[0].Text != "restored" {
		t.Fatalf("%+v %v", msg, err)
	}
	if err = p.EditMessage(context.Background(), cap.EditRequest{Message: ref, Content: []cap.Part{{Type: "text", Text: "updated"}}}); err != nil {
		t.Fatal(err)
	}
	topic, err := p.CreateTopic(context.Background(), cap.PublishRequest{Route: businessSlackRoute(), Content: []cap.Part{{Type: "text", Text: "root"}}})
	target, _ := rctx.DecodeSessionRoute(topic.Route)
	if err != nil || target.ThreadID != "100.3" {
		t.Fatalf("%+v %v", topic, err)
	}
}
func TestBusinessMessagesUnknownAndRateLimit(t *testing.T) {
	for _, mode := range []string{"unknown", "rate"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if mode == "rate" {
					w.Header().Set("Retry-After", "3")
					w.WriteHeader(429)
				} else {
					w.WriteHeader(502)
				}
			}))
			defer srv.Close()
			p := &Platform{client: slackapi.New("token", slackapi.OptionAPIURL(srv.URL+"/"))}
			p.SetPlatformInstance("business")
			receipt, err := p.Publish(context.Background(), cap.PublishRequest{Route: businessSlackRoute(), Content: []cap.Part{{Type: "text", Text: "hi"}}})
			var de *cap.Error
			if !errors.As(err, &de) || calls != 1 {
				t.Fatalf("%+v %v calls=%d", receipt, err, calls)
			}
			if mode == "unknown" && receipt.Outcome != cap.OutcomeUnknown {
				t.Fatalf("%+v", receipt)
			}
			if mode == "rate" && (de.Code != cap.CodeRateLimited || de.RetryAfter == 0 || receipt.Outcome != cap.OutcomeNotSent) {
				t.Fatalf("%+v %+v", receipt, de)
			}
		})
	}
}
