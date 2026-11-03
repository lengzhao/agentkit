package feishu

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

func TestProgressCreatePublishesHandleBeforeNextCall(t *testing.T) {
	var posts atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_one","chat_id":"oc_chat"}}`)
	}))
	defer srv.Close()
	p.useInteractiveCard = true
	first, err := p.createProgressCard(context.Background(), p.streamState("turn"), replyContext{chatID: "oc_chat"}, "progress")
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.createProgressCard(context.Background(), p.streamState("turn"), replyContext{chatID: "oc_chat"}, "progress")
	if err != nil || first != second || posts.Load() != 1 {
		t.Fatalf("first=%p second=%p posts=%d err=%v", first, second, posts.Load(), err)
	}
}

func TestTurnEndKeepsUnknownCreateAfterStateCleanup(t *testing.T) {
	var posts atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) { posts.Add(1); w.WriteHeader(502) }))
	defer srv.Close()
	p.useInteractiveCard = true
	key := agentkit.SessionID("lark:oc_chat")
	st := p.richStreamState(key)
	st.lock()
	st.bodyText = "answer"
	st.unknownCreateErr = &messageWriteError{Operation: "create", Unknown: true}
	st.unlock()
	if err := p.handleRichTurnEnd(context.Background(), key, capsession.TurnEndData{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, exists := p.streams.Load(key); exists || posts.Load() != 0 {
		t.Fatalf("ended state recreated=%v posts=%d", exists, posts.Load())
	}
}

func TestTurnEndWaitsForInflightCreate(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown=%v", unknown), func(t *testing.T) {
			entered, release, patched := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
			var posts atomic.Int32
			p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "/cardkit/") {
					fmt.Fprint(w, `{"code":230001}`)
					return
				}
				if r.Method == http.MethodPost {
					if posts.Add(1) == 1 {
						close(entered)
						<-release
					}
					if unknown {
						w.WriteHeader(502)
						return
					}
					fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_one","chat_id":"oc_chat"}}`)
					return
				}
				if r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "om_one") {
					patched <- struct{}{}
					fmt.Fprint(w, `{"code":0}`)
					return
				}
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				fmt.Fprint(w, `{"code":0}`)
			}))
			defer srv.Close()
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			p.useInteractiveCard = true
			key := agentkit.SessionID("lark:oc_chat")
			st := p.richStreamState(key)
			st.lock()
			st.bodyText = "answer"
			st.unlock()
			flushed := make(chan error, 1)
			go func() { flushed <- p.flushRichCard(context.Background(), key, true) }()
			<-entered
			if err := p.handleRichTurnEnd(context.Background(), key, capsession.TurnEndData{}); err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-flushed; err != nil {
				t.Fatal(err)
			}
			if !unknown {
				select {
				case <-patched:
				case <-time.After(time.Second):
					t.Fatal("finalizer did not patch completed creation")
				}
			} else {
				time.Sleep(100 * time.Millisecond)
			}
			if _, exists := p.streams.Load(key); exists || posts.Load() != 1 {
				t.Fatalf("ended state recreated=%v posts=%d", exists, posts.Load())
			}
		})
	}
}

func TestEndedStreamTimersCannotRecreateOrTouchNextTurn(t *testing.T) {
	p := &Platform{}
	key := agentkit.SessionID("timer-reused")
	old := p.streamState(key)
	old.lock()
	old.progressHandle = &feishuPreviewHandle{messageID: "old"}
	old.unlock()
	p.clearStreamState(key)
	next := p.streamState(key)
	p.scheduleRichCardKeepalive(key, old)
	p.scheduleBodyFlush(key, old)
	p.onRichCardKeepalive(key, old)
	old.lock()
	defer old.unlock()
	if old.keepaliveTimer != nil || old.bodyFlushTimer != nil {
		t.Fatal("ended state restarted timers")
	}
	actual, _ := p.streams.Load(key)
	if actual != next {
		t.Fatal("old timer changed next turn state")
	}
}
