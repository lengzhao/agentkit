package feishu

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMessageCreateUnknownIsNotRetried(t *testing.T) {
	for _, status := range []int{408, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(status)
				fmt.Fprint(w, "invalid access token after simulated commit")
			}))
			defer srv.Close()
			p.domain = srv.URL
			err := p.withTransientRetry(context.Background(), "outer", func() error {
				return p.createMessage(context.Background(), "oc_chat", "text", `{"text":"once"}`, "send")
			})
			if !isMessageWriteUnknown(err) || calls.Load() != 1 {
				t.Fatalf("unknown create retried: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}
func TestMessageReplyNeverChangesTargetAfterFailure(t *testing.T) {
	for _, mode := range []string{"unknown", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if !strings.HasSuffix(r.URL.Path, "/reply") {
					t.Errorf("reply changed target: %s", r.URL.Path)
				}
				if mode == "unknown" {
					w.Header().Set("Content-Type", "text/plain")
					w.WriteHeader(502)
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"code":230002,"msg":"not in group"}`)
				}
			}))
			defer srv.Close()
			err := p.replyMessage(context.Background(), replyContext{chatID: "oc_chat", messageID: "om_root", chatType: "group"}, "text", `{"text":"once"}`)
			if err == nil || calls.Load() != 1 || isMessageWriteUnknown(err) != (mode == "unknown") {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}
func TestMessageWriteRefreshesOnlyConfirmedTokenRejection(t *testing.T) {
	var writes atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if writes.Add(1) == 1 {
			fmt.Fprint(w, `{"code":99991663,"msg":"invalid access token"}`)
		} else {
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_sent","chat_id":"oc_chat"}}`)
		}
	}))
	defer srv.Close()
	p.domain = srv.URL
	if err := p.createMessage(context.Background(), "oc_chat", "text", `{"text":"sent"}`, "send"); err != nil || writes.Load() != 2 {
		t.Fatalf("writes=%d err=%v", writes.Load(), err)
	}
}
func TestMessageWriteCanceledBeforeRequest(t *testing.T) {
	var calls atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.createMessage(ctx, "oc_chat", "text", `{"text":"cancelled"}`, "send")
	if !errors.Is(err, context.Canceled) || isMessageWriteUnknown(err) || calls.Load() != 0 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}

func TestPreviewUnknownDoesNotFallbackToChat(t *testing.T) {
	var calls atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(502)
	}))
	defer srv.Close()
	p.useInteractiveCard = true
	_, err := p.SendPreviewStart(context.Background(), replyContext{chatID: "oc_chat", messageID: "om_root", chatType: "group"}, "progress")
	if !isMessageWriteUnknown(err) || calls.Load() != 1 {
		t.Fatalf("preview repeated/changed target: calls=%d err=%v", calls.Load(), err)
	}
}
func TestUnknownProgressCreateIsNotRebuilt(t *testing.T) {
	var calls atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(502)
	}))
	defer srv.Close()
	p.useInteractiveCard = true
	for range 2 {
		_, err := p.createProgressCard(context.Background(), p.streamState("turn"), replyContext{chatID: "oc_chat"}, "progress")
		if !isMessageWriteUnknown(err) {
			t.Fatalf("unknown create changed to cooldown/success: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("unknown card creation repeated: %d", calls.Load())
	}
}
func TestFinalPlainUnknownIsNotRetried(t *testing.T) {
	delays := finalPlainTextRetryDelays
	finalPlainTextRetryDelays = []time.Duration{0, 0}
	defer func() { finalPlainTextRetryDelays = delays }()
	var calls atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(502)
	}))
	defer srv.Close()
	p.sendFinalPlainTextWithRetry(context.Background(), "lark:oc_chat", "turn", "final")
	if calls.Load() != 1 {
		t.Fatalf("uncertain final plain message retried: %d", calls.Load())
	}
}

func TestProgressCardCreationSerializesConcurrentFlushes(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_card","chat_id":"oc_chat"}}`)
	}))
	defer srv.Close()
	p.useInteractiveCard = true
	first := make(chan error, 1)
	go func() {
		_, err := p.createProgressCard(context.Background(), p.streamState("turn"), replyContext{chatID: "oc_chat"}, "progress")
		first <- err
	}()
	<-entered
	second := make(chan error, 1)
	go func() {
		_, err := p.createProgressCard(context.Background(), p.streamState("turn"), replyContext{chatID: "oc_chat"}, "progress")
		second <- err
	}()
	select {
	case err := <-second:
		close(release)
		if !errors.Is(err, errRichCardCreateCooldown) {
			t.Fatalf("second flush should wait for known handle: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		close(release)
		<-first
		<-second
		t.Fatal("concurrent flush attempted another message creation")
	}
	if err := <-first; err != nil || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}
