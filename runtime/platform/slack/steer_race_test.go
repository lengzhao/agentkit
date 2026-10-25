package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/rctx"
	slackapi "github.com/slack-go/slack"
)

func TestTurnEndDuringSteerSwapClearsLateReaction(t *testing.T) {
	entered, release, ended := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	reactions := map[string]bool{reactionReceived: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		name := r.Form.Get("name")
		if r.URL.Path == "/reactions.add" && name == reactionSteer {
			close(entered)
			<-release
		}
		mu.Lock()
		if r.URL.Path == "/reactions.add" {
			reactions[name] = true
		} else {
			delete(reactions, name)
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		if name == reactionDone {
			close(ended)
		}
	}))
	defer server.Close()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	p := &Platform{steerEmoji: reactionSteer, doneEmoji: reactionDone, client: slackapi.New("test", slackapi.OptionAPIURL(server.URL+"/"))}
	id := agentkit.SessionID("slack:C1")
	p.beginTurnReactions(id, delivery{channel: "C1", msgTS: "2.0", sessionID: id})
	event := agentkit.OutboundEvent{Type: agentkit.EventTurnSteer, Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "slack", DeliveryID: id, ChannelID: "C1", ReplyTo: "2.0"})}
	done := make(chan struct{})
	go func() { defer close(done); _ = p.applyTurnSteer(context.Background(), event) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("steer API not reached")
	}
	p.applyTurnEndReactions(context.Background(), id, capsession.TurnEndData{})
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("turn end did not finish")
	}
	unblock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("steer did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if reactions[reactionSteer] || reactions[reactionReceived] || !reactions[reactionDone] {
		t.Fatalf("unexpected completed reactions: %v", reactions)
	}
}
