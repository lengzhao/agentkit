package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	caplarkevent "github.com/lengzhao/agentkit/cap/larkevent"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func TestEventQueueAdmissionFailureDoesNotConsumeDedup(t *testing.T) {
	p := &Platform{platformTag: "lark", dedup: &common.MessageDedup{}}
	p.ingressOnce.Do(func() { p.ingressWork = common.NewKeyedExecutor(1, 1) })
	defer p.ingressExecutor().Stop(context.Background())
	started, release, drained := make(chan struct{}), make(chan struct{}), make(chan struct{})
	_ = p.queueIngress("busy", func(context.Context) { close(started); <-release })
	<-started
	_ = p.queueIngress("busy", func(context.Context) { close(drained) })
	h := &recordingHandler{handled: make(chan string, 1)}
	route := &customEventRoute{handlers: []caplarkevent.Handler{h}}
	req := &larkevent.EventReq{Body: []byte(`{"header":{"event_id":"admission"},"event":{"chat_id":"chat"}}`)}
	if err := p.dispatchCustomEvent("test", route, req); !errors.Is(err, common.ErrExecutorFull) {
		t.Fatalf("err=%v", err)
	}
	close(release)
	<-drained
	if err := p.dispatchCustomEvent("test", route, req); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-h.handled:
		if id != "admission" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("retry was discarded as a duplicate")
	}
}

type cancellationEventHandler struct{ started, finished chan struct{} }

func (h *cancellationEventHandler) EventTypes() []string { return []string{"test"} }
func (h *cancellationEventHandler) Handle(ctx context.Context, _ caplarkevent.Event) error {
	close(h.started)
	<-ctx.Done()
	close(h.finished)
	return ctx.Err()
}
func TestCustomEventUsesPlatformLifecycle(t *testing.T) {
	p := &Platform{platformTag: "lark", dedup: &common.MessageDedup{}}
	host, cancel := context.WithCancel(context.Background())
	p.ingressExecutor().Bind(host)
	h := &cancellationEventHandler{started: make(chan struct{}), finished: make(chan struct{})}
	err := p.dispatchCustomEvent("test", &customEventRoute{handlers: []caplarkevent.Handler{h}}, &larkevent.EventReq{Body: []byte(`{"header":{"event_id":"lifecycle"},"event":{}}`)})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.started:
	case <-time.After(time.Second):
		t.Fatal("handler did not run")
	}
	cancel()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err = p.ingressExecutor().Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.finished:
	default:
		t.Fatal("handler detached from platform lifetime")
	}
}

func TestMessageQueueAdmissionFailureDoesNotConsumeDedup(t *testing.T) {
	p := &Platform{platformTag: "lark", dedup: &common.MessageDedup{}, inbox: common.NewInbox(1)}
	p.chatNameCache.Store("chat", "Chat")
	p.userProfileCache.Store("user", feishuUserProfileEntry{})
	p.ingressOnce.Do(func() { p.ingressWork = common.NewKeyedExecutor(1, 1) })
	defer p.ingressExecutor().Stop(context.Background())
	started, release, drained := make(chan struct{}), make(chan struct{}), make(chan struct{})
	_ = p.queueIngress("busy", func(context.Context) { close(started); <-release })
	<-started
	_ = p.queueIngress("busy", func(context.Context) { close(drained) })
	var event larkim.P2MessageReceiveV1
	if err := json.Unmarshal([]byte(`{"event":{"sender":{"sender_id":{"open_id":"user"}},"message":{"message_id":"admit-message","chat_id":"chat","chat_type":"p2p","message_type":"text","content":"{\"text\":\"hello\"}"}}}`), &event); err != nil {
		t.Fatal(err)
	}
	if err := p.onMessage(context.Background(), &event); !errors.Is(err, common.ErrExecutorFull) {
		t.Fatalf("err=%v", err)
	}
	close(release)
	<-drained
	if err := p.onMessage(context.Background(), &event); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := p.inbox.Receive(ctx)
	if err != nil || len(got.Message.Content) == 0 || got.Message.Content[0].Text != "hello" {
		t.Fatalf("event=%+v err=%v", got, err)
	}
}

func TestDispatchCoreMessagePreservesCancellation(t *testing.T) {
	p := &Platform{platformTag: "lark", inbox: common.NewInbox(1)}
	p.userProfileCache.Store("user", feishuUserProfileEntry{})
	if err := p.inbox.Push(context.Background(), agentkit.MessageEvent{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		p.dispatchCoreMessage(ctx, &inboundMessage{sessionID: "lark:chat:u:user", messageID: "message", userID: "user", content: "hello", rctx: replyContext{chatID: "chat", chatType: "p2p", sessionKey: "lark:chat:u:user"}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("core message detached from canceled executor context")
	}
}

func TestIngressResourceRequestStopsWithExecutor(t *testing.T) {
	for _, kind := range []string{"image", "file"} {
		t.Run(kind, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Path, "/resources/") {
					t.Errorf("unexpected request: %s", r.URL.Path)
					return
				}
				close(started)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer func() { close(release); srv.Close() }()
			p.chatNameCache.Store("chat", "Chat")
			p.userProfileCache.Store("user", feishuUserProfileEntry{})
			p.inbox = common.NewInbox(1)
			host, cancel := context.WithCancel(context.Background())
			defer cancel()
			p.ingressExecutor().Bind(host)
			if err := p.queueIngress("chat", func(ctx context.Context) {
				p.dispatchMessage(ctx, kind, `{"image_key":"image","file_key":"file","file_name":"file.txt"}`, nil, "message", "lark:chat:u:user", "user", "chat", replyContext{chatID: "chat", chatType: "p2p", sessionKey: "lark:chat:u:user"}, "")
			}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("resource request did not start")
			}
			cancel()
			ctx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			if err := p.ingressExecutor().Stop(ctx); err != nil {
				t.Fatalf("resource IO outlived executor cancellation: %v", err)
			}
		})
	}
}
