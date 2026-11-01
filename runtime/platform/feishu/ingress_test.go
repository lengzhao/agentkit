package feishu

import (
	"context"
	"errors"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/ingress"
	"github.com/lengzhao/agentkit/cap/interaction"
	caplarkevent "github.com/lengzhao/agentkit/cap/larkevent"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"net/http"
	"strings"
	"testing"
	"time"
)

type testIngressPolicy func(context.Context, ingress.Input) (ingress.Decision, error)

func (f testIngressPolicy) Evaluate(ctx context.Context, in ingress.Input) (ingress.Decision, error) {
	return f(ctx, in)
}
func TestIngressPolicyDefaultsAndErrors(t *testing.T) {
	p := &Platform{}
	in := ingress.Input{Kind: ingress.Message, Default: ingress.Plan{Agent: ingress.Submit}}
	if got := p.evaluateIngress(context.Background(), in); got.Agent != ingress.Submit {
		t.Fatal(got)
	}
	p.inboundPolicy = testIngressPolicy(func(context.Context, ingress.Input) (ingress.Decision, error) {
		return ingress.Decision{}, errors.New("unavailable")
	})
	if got := p.evaluateIngress(context.Background(), in); got.Agent != ingress.Skip {
		t.Fatal(got)
	}
}
func TestIngressContextUsesActiveLogicalSession(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p := &Platform{platformTag: "lark", sessionStore: store, workspace: ws}
	rc := replyContext{chatID: "C1", chatType: "p2p", sessionKey: "lark:C1:U1"}
	owner, state := p.ingressContext(context.Background(), rc, "U1")
	if owner == "" || state != ingress.New {
		t.Fatal(owner, state)
	}
	p.rememberIngressText(owner)
	_, state = p.ingressContext(context.Background(), rc, "U1")
	if state != ingress.Existing {
		t.Fatal(state)
	}
	route, _, _ := p.ingressEnvelope(context.Background(), rc, "U1")
	_ = route
	// /new changes logical ownership even though the delivery route is unchanged.
	if err := store.(interface {
		SetActiveSession(context.Context, agentkit.SessionID, agentkit.SessionID) error
	}).SetActiveSession(context.Background(), owner, "fresh-session"); err != nil {
		t.Fatal(err)
	}
	next, state := p.ingressContext(context.Background(), rc, "U1")
	if next == owner || state != ingress.New {
		t.Fatal(next, state)
	}
}
func TestDeferredAttachmentWaitsAndKeepsUserIsolation(t *testing.T) {
	p := &Platform{platformTag: "lark", inbox: common.NewInbox(2)}
	defer p.flushDeferredAttachments()
	rc := replyContext{chatID: "C1", chatType: "p2p", messageID: "file1", sessionKey: "lark:C1:t:file1:U1"}
	if err := p.deferAttachment(context.Background(), rc, "U1", "owner1", []byte("hello"), "text/plain", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok, ambiguous := p.deferredRoute(rc, "U2", true); ok || ambiguous {
		t.Fatal("another user received pending file")
	}
	follow := replyContext{chatID: "C1", chatType: "p2p", messageID: "text2", sessionKey: "lark:C1:t:text2:U1"}
	target, ok, ambiguous := p.deferredRoute(follow, "U1", true)
	if !ok || ambiguous || target.sessionKey != rc.sessionKey {
		t.Fatal(target, ok, ambiguous)
	}
	msg := inboundMessage{sessionID: "lark:C1:t:file1:U1", userID: "U1", content: "summarize", rctx: target}
	if err := p.consumeDeferred(&msg, "owner1"); err != nil {
		t.Fatal(err)
	}
	p.commitDeferred(msg, "owner1")
	if len(msg.files) != 1 || string(msg.files[0].Data) != "hello" {
		t.Fatal(msg.files)
	}
	if _, ok, _ := p.deferredRoute(follow, "U1", true); ok {
		t.Fatal("consumed file survived")
	}
}
func TestDeferredAttachmentDoesNotCrossNewSession(t *testing.T) {
	p := &Platform{platformTag: "lark"}
	defer p.flushDeferredAttachments()
	rc := replyContext{chatID: "C1", sessionKey: "lark:C1:U1"}
	if err := p.deferAttachment(context.Background(), rc, "U1", "old", []byte("hello"), "text/plain", "a.txt"); err != nil {
		t.Fatal(err)
	}
	msg := inboundMessage{userID: "U1", rctx: rc}
	if err := p.consumeDeferred(&msg, "new"); err == nil {
		t.Fatal("old attachments crossed /new")
	}
	if _, ok, _ := p.deferredRoute(rc, "U1", false); !ok {
		t.Fatal("failure consumed files")
	}
}

func TestIngressEventCanDisableHandlers(t *testing.T) {
	p := &Platform{platformTag: "feishu", dedup: &common.MessageDedup{}}
	evaluated := make(chan struct{})
	p.inboundPolicy = testIngressPolicy(func(_ context.Context, in ingress.Input) (ingress.Decision, error) {
		if in.Kind != ingress.Event || in.ConversationKind != "unknown" {
			t.Errorf("unexpected input: %+v", in)
		}
		close(evaluated)
		return ingress.Decision{Handlers: ingress.Disable}, nil
	})
	h := &recordingHandler{handled: make(chan string, 1)}
	p.dispatchCustomEvent("example", &customEventRoute{handlers: []caplarkevent.Handler{h}}, &larkevent.EventReq{Body: []byte(`{"header":{"event_id":"e1"},"event":{}}`)})
	select {
	case <-evaluated:
	case <-time.After(time.Second):
		t.Fatal("policy not evaluated")
	}
	select {
	case <-h.handled:
		t.Fatal("disabled handler was called")
	case <-time.After(30 * time.Millisecond):
	}
}
func TestIngressInteractionCanDisableHandlersAndAgent(t *testing.T) {
	p, h := interactionPlatform()
	p.inbox = common.NewInbox(1)
	p.inboundPolicy = testIngressPolicy(func(_ context.Context, in ingress.Input) (ingress.Decision, error) {
		return ingress.Decision{Agent: ingress.Skip, Handlers: ingress.Disable}, nil
	})
	if err := p.processCardInteraction(interaction.Event{Platform: "feishu", ChatID: "oc_c", UserID: "ou_u", Action: "custom"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.events:
		t.Fatal("disabled handler called")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.inbox.Receive(ctx); err == nil {
		t.Fatal("disabled agent forwarded")
	}
}

func TestIngressMessageACLBeforePolicy(t *testing.T) {
	p := &Platform{platformTag: "feishu", allowChat: "allowed", inbox: common.NewInbox(1)}
	p.inboundPolicy = testIngressPolicy(func(context.Context, ingress.Input) (ingress.Decision, error) {
		t.Error("policy ran for forbidden chat")
		return ingress.Decision{Agent: ingress.Submit}, nil
	})
	p.dispatchMessage(context.Background(), "text", `{"text":"hello"}`, nil, "m", "feishu:blocked:u", "u", "blocked", replyContext{chatType: "group", chatID: "blocked", sessionKey: "feishu:blocked:u"}, "")
}
func TestIngressMessageCanOverrideMentionRequirement(t *testing.T) {
	p := &Platform{platformTag: "feishu", botOpenID: "bot", inbox: common.NewInbox(1)}
	p.userProfileCache.Store("u", feishuUserProfileEntry{})
	p.chatNameCache.Store("c", "test")
	p.inboundPolicy = testIngressPolicy(func(_ context.Context, in ingress.Input) (ingress.Decision, error) {
		if in.Default.Agent != ingress.Skip || in.Message.AddressedToAgent {
			t.Error("lost normal trigger facts")
		}
		return ingress.Decision{Agent: ingress.Submit}, nil
	})
	p.dispatchMessage(context.Background(), "text", `{"text":"hello"}`, nil, "m", "feishu:c:u", "u", "c", replyContext{chatType: "group", chatID: "c", sessionKey: "feishu:c:u"}, "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := p.inbox.Receive(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestDeferredAttachmentAmbiguityAndExpiry(t *testing.T) {
	p := &Platform{platformTag: "lark", pendingTTL: 20 * time.Millisecond}
	defer p.flushDeferredAttachments()
	for _, id := range []string{"one", "two"} {
		rc := replyContext{chatID: "c", chatType: "p2p", sessionKey: id}
		if err := p.deferAttachment(context.Background(), rc, "u", "owner", []byte("x"), "text/plain", "x"); err != nil {
			t.Fatal(err)
		}
	}
	rc := replyContext{chatID: "c", chatType: "p2p", sessionKey: "next"}
	if _, found, ambiguous := p.deferredRoute(rc, "u", true); found || !ambiguous {
		t.Fatal(found, ambiguous)
	}
	time.Sleep(50 * time.Millisecond)
	if _, found, ambiguous := p.deferredRoute(rc, "u", true); found || ambiguous {
		t.Fatal("expired files survived")
	}
}
func TestQueueIngressPreservesReceiveOrder(t *testing.T) {
	p := &Platform{}
	first := make(chan struct{})
	second := make(chan struct{})
	p.queueIngress("chat/user", func() { <-first })
	p.queueIngress("chat/user", func() { close(second) })
	select {
	case <-second:
		t.Fatal("second input overtook upload")
	case <-time.After(10 * time.Millisecond):
	}
	close(first)
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("queue stalled")
	}
}
func TestIngressPolicyDeadlineFailsClosed(t *testing.T) {
	p := &Platform{inboundPolicy: testIngressPolicy(func(ctx context.Context, _ ingress.Input) (ingress.Decision, error) {
		<-ctx.Done()
		return ingress.Decision{Agent: ingress.Submit}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := p.evaluateIngress(ctx, ingress.Input{Kind: ingress.Message, Default: ingress.Plan{Agent: ingress.Submit}}); got.Agent != ingress.Skip {
		t.Fatal(got)
	}
}

func TestDeferredUnknownOwnerNeverAutoConsumes(t *testing.T) {
	p := &Platform{}
	defer p.flushDeferredAttachments()
	rc := replyContext{sessionKey: "route"}
	if err := p.deferAttachment(context.Background(), rc, "u", "", []byte("x"), "text/plain", "a"); err != nil {
		t.Fatal(err)
	}
	if err := p.consumeDeferred(&inboundMessage{userID: "u", rctx: rc}, ""); err == nil {
		t.Fatal("unverifiable owner consumed")
	}
}
func TestDeferredFilesRemainUntilQueueAcceptance(t *testing.T) {
	p := &Platform{}
	defer p.flushDeferredAttachments()
	rc := replyContext{sessionKey: "route"}
	if err := p.deferAttachment(context.Background(), rc, "u", "owner", []byte("x"), "text/plain", "a"); err != nil {
		t.Fatal(err)
	}
	msg := inboundMessage{userID: "u", rctx: rc}
	if err := p.consumeDeferred(&msg, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := p.deferredRoute(rc, "u", false); !found {
		t.Fatal("files removed before queue acceptance")
	}
}

func TestIngressDMFilesWaitMergeAndResumeOriginalTopic(t *testing.T) {
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/resources/") {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("file payload"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"message_id":"receipt"}}`))
	}))
	defer srv.Close()
	defer p.flushDeferredAttachments()
	p.cfg.DMThreadMode = true
	p.inbox = common.NewInbox(2)
	p.workspace = rtworkspace.Static(t.TempDir())
	p.sessionScope = agentkit.SessionScopeThread
	p.userProfileCache.Store("U1", feishuUserProfileEntry{})
	p.chatNameCache.Store("C1", "test")
	p.inboundPolicy = testIngressPolicy(func(_ context.Context, in ingress.Input) (ingress.Decision, error) {
		if in.Message.AttachmentOnly {
			return ingress.Decision{Agent: ingress.Defer}, nil
		}
		return ingress.Decision{Agent: ingress.Submit}, nil
	})
	key := string(rctx.BuildDeliverySessionID("lark", "C1", "file1", "U1"))
	rc := replyContext{chatID: "C1", chatType: "p2p", messageID: "file1", sessionKey: key}
	p.dispatchMessage(context.Background(), "file", `{"file_key":"f1","file_name":"a.txt"}`, nil, "file1", key, "U1", "C1", rc, "")
	key2 := string(rctx.BuildDeliverySessionID("lark", "C1", "file2", "U1"))
	p.dispatchMessage(context.Background(), "file", `{"file_key":"f2","file_name":"b.txt"}`, nil, "file2", key2, "U1", "C1", replyContext{chatID: "C1", chatType: "p2p", messageID: "file2", sessionKey: key2}, "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.inbox.Receive(ctx); err == nil {
		t.Fatal("files triggered agent before instruction")
	}
	key3 := string(rctx.BuildDeliverySessionID("lark", "C1", "text3", "U1"))
	p.dispatchMessage(context.Background(), "text", `{"text":"summarize both"}`, nil, "text3", key3, "U1", "C1", replyContext{chatID: "C1", chatType: "p2p", messageID: "text3", sessionKey: key3}, "")
	receiveCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	event, err := p.inbox.Receive(receiveCtx)
	if err != nil {
		t.Fatal(err)
	}
	if len(event.Uploads) != 2 || rctx.InboundDeliveryID(event) != agentkit.SessionID(key) {
		t.Fatalf("lost merged files or original topic: %+v", event)
	}
}

func TestIngressPostInstructionFacts(t *testing.T) {
	for _, raw := range []string{
		`{"title":"please summarize","content":[[{"tag":"img","image_key":"i"}]]}`,
		`{"content":[[{"tag":"markdown","text":"please summarize"}]]}`,
		`{"en_us":{"content":[[{"tag":"code_block","text":"fmt.Println(1)"}]]}}`,
		`{"content":[[{"tag":"a","text":"documentation","href":"https://example.com"}]]}`,
	} {
		if text := instructionText("post", raw, nil, "bot"); text == "" {
			t.Fatalf("lost rich text instructions: %s", raw)
		}
	}
	if text := instructionText("post", `{"content":[[{"tag":"img","image_key":"i"}]]}`, nil, "bot"); text != "" {
		t.Fatal(text)
	}
}

func TestNewSessionRetiresOldDeferredFiles(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p := &Platform{platformTag: "lark", sessionStore: store, workspace: ws}
	defer p.flushDeferredAttachments()
	rc := replyContext{chatID: "c", chatType: "p2p", sessionKey: "lark:c:u"}
	owner, _ := p.ingressContext(context.Background(), rc, "u")
	if err := p.deferAttachment(context.Background(), rc, "u", owner, []byte("old"), "text/plain", "old"); err != nil {
		t.Fatal(err)
	}
	if err := store.(agentkit.ActiveSessionStore).SetActiveSession(context.Background(), owner, "fresh"); err != nil {
		t.Fatal(err)
	}
	p.pruneDeferredAttachments(context.Background(), rc, "u")
	if _, found, _ := p.deferredRoute(rc, "u", false); found {
		t.Fatal("old pending blocked new session")
	}
	next, _ := p.ingressContext(context.Background(), rc, "u")
	if err := p.deferAttachment(context.Background(), rc, "u", next, []byte("new"), "text/plain", "new"); err != nil {
		t.Fatal(err)
	}
	msg := inboundMessage{rctx: rc, userID: "u"}
	if err := p.consumeDeferred(&msg, next); err != nil {
		t.Fatal(err)
	}
	if len(msg.files) != 1 || string(msg.files[0].Data) != "new" {
		t.Fatal(msg.files)
	}
}

func TestIngressHistoryIgnoresAttachmentPlaceholder(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	p := &Platform{platformTag: "lark", workspace: ws, sessionStore: store}
	rc := replyContext{chatID: "c", sessionKey: "lark:c:u"}
	env, ctx, err := p.ingressEnvelope(context.Background(), rc, "u")
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.Get(ctx, agentkit.SessionID(env.Conversation))
	if err != nil {
		t.Fatal(err)
	}
	appendUser := func(text string) {
		t.Helper()
		if err := sessevents.Default.AppendMessage(ctx, session, "a", agentkit.EventUserMessage, agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: text}}}); err != nil {
			t.Fatal(err)
		}
	}
	appendUser("Please analyze the attached file(s).\n\n(Files the user uploaded with this message. workspace paths:\n- a.txt)")
	if _, state := p.ingressContext(context.Background(), rc, "u"); state != ingress.New {
		t.Fatal(state)
	}
	appendUser("summarize my report")
	if _, state := p.ingressContext(context.Background(), rc, "u"); state != ingress.Existing {
		t.Fatal(state)
	}
}
