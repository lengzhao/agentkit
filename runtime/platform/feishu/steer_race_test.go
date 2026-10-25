package feishu

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestTurnEndDuringSteerSwapClearsLateReaction(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	removed := false
	p, server := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			close(entered)
			<-release
			_, _ = w.Write([]byte(`{"code":0,"data":{"reaction_id":"steer-id"}}`))
			return
		}
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/steer-id") {
			mu.Lock()
			removed = true
			mu.Unlock()
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer server.Close()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	p.steerEmoji = "Typing"
	id := agentkit.SessionID("lark:oc_1")
	p.beginTurnReactions(id, replyContext{messageID: "om_2", processingReactionID: "old-id"})
	event := agentkit.OutboundEvent{Type: agentkit.EventTurnSteer, Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", DeliveryID: id, ChannelID: "oc_1", ReplyTo: "om_2"})}
	done := make(chan struct{})
	go func() { defer close(done); _ = p.applyTurnSteer(event) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("steer API not reached")
	}
	// turn/end has already taken the old reaction ID for cleanup.
	snapshot := p.finishTurnReactions(id)
	if len(snapshot) != 1 || snapshot[0].processingReactionID != "old-id" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	unblock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("steer did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if !removed {
		t.Fatal("late steer reaction was never removed after turn/end")
	}
}
