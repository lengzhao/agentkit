package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/interaction"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type testInteractionHandler struct{ events chan interaction.Event }

func (h *testInteractionHandler) HandleInteraction(_ context.Context, e interaction.Event) (bool, error) {
	h.events <- e
	return true, nil
}
func testCardEvent(action string) *callback.CardActionTriggerEvent {
	return &callback.CardActionTriggerEvent{Event: &callback.CardActionTriggerRequest{Operator: &callback.Operator{OpenID: "ou_u"}, Context: &callback.Context{OpenChatID: "oc_c", OpenMessageID: "om_m"}, Action: &callback.CallBackAction{Name: "answer", Tag: "select_static", Value: map[string]any{"action": action}, Option: "v1"}}}
}
func interactionPlatform() (*Platform, *testInteractionHandler) {
	h := &testInteractionHandler{events: make(chan interaction.Event, 10)}
	return &Platform{platformTag: "feishu", dedup: &common.MessageDedup{}, interactionHandlers: []interaction.Handler{h}}, h
}
func waitInteraction(t *testing.T, h *testInteractionHandler) interaction.Event {
	t.Helper()
	select {
	case e := <-h.events:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("missing interaction")
		return interaction.Event{}
	}
}
func TestUnhandledCardPrefixesReachPlugins(t *testing.T) {
	for _, a := range []string{"act:/external", "nav:/external", "cmd:/external", "custom"} {
		t.Run(a, func(t *testing.T) {
			p, h := interactionPlatform()
			resp, err := p.onCardAction(testCardEvent(a))
			if err != nil || resp == nil || resp.Toast == nil {
				t.Fatalf("missing ack: %v %v", resp, err)
			}
			e := waitInteraction(t, h)
			if e.Action != a || e.CardStatus != interaction.CardUnavailable || !strings.Contains(string(e.RawCallback), "v1") {
				t.Fatalf("incomplete event: %+v", e)
			}
		})
	}
}
func TestCardDedupIncludesAllAnswers(t *testing.T) {
	p, h := interactionPlatform()
	ev := testCardEvent("custom")
	p.onCardAction(ev)
	waitInteraction(t, h)
	p.onCardAction(ev)
	ev.Event.Action.Option = "v2"
	p.onCardAction(ev)
	e := waitInteraction(t, h)
	if !strings.Contains(string(e.RawAction), "v2") {
		t.Fatal(string(e.RawAction))
	}
	select {
	case <-h.events:
		t.Fatal("duplicate dispatched")
	case <-time.After(50 * time.Millisecond):
	}
}
func TestCardACLAndPermissionNeverReachPlugins(t *testing.T) {
	for _, kind := range []string{"user", "chat", "permission", "malformed_permission"} {
		t.Run(kind, func(t *testing.T) {
			p, h := interactionPlatform()
			p.inbox = common.NewInbox(4)
			ev := testCardEvent("custom")
			switch kind {
			case "user":
				p.allowFrom = "someone_else"
			case "chat":
				p.allowChat = "another_chat"
			case "permission":
				ev.Event.Action.Value = map[string]any{"action": "perm:allow", "request_id": "r1"}
			case "malformed_permission":
				ev.Event.Action.Value = map[string]any{"action": "perm:allow"}
			}
			p.onCardAction(ev)
			select {
			case <-h.events:
				t.Fatal("forbidden or permission reached plugin")
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}
func TestFeishuAnswersRetainEmptyInputAndForm(t *testing.T) {
	ev := testCardEvent("submit")
	ev.Event.Action.InputValue = ""
	ev.Event.Action.FormValue = map[string]any{"env": []any{"a", "b"}, "comment": "hello"}
	e := feishuInteractionSnapshot(ev, "s", "c", "m", "u", "submit")
	raw, _ := json.Marshal(e.Answers)
	for _, want := range []string{"env", "a", "b", "hello"} {
		if !strings.Contains(string(raw), want) {
			t.Fatal(string(raw))
		}
	}
}

func TestCardSnapshotOwnsAnswers(t *testing.T) {
	ev := testCardEvent("submit")
	ev.Event.Action.FormValue = map[string]any{"nested": map[string]any{"comment": "before"}}
	e := feishuInteractionSnapshot(ev, "s", "c", "m", "u", "submit")
	ev.Event.Action.Value["action"] = "mutated"
	ev.Event.Action.FormValue["nested"].(map[string]any)["comment"] = "after"
	raw, _ := json.Marshal(e.Answers)
	if strings.Contains(string(raw), "mutated") || strings.Contains(string(raw), "after") {
		t.Fatalf("snapshot aliases SDK maps: %s", raw)
	}
}
func TestCardCallbackAcknowledgesBeforeQueryAndRestoresThread(t *testing.T) {
	blocked := make(chan struct{})
	started := make(chan struct{}, 1)
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/messages/") {
			started <- struct{}{}
			<-blocked
			content := `{"header":{"title":{"content":"部署"}},"elements":[{"name":"answer","options":[{"value":"v1","text":{"content":"生产环境"}}]}]}`
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []any{map[string]any{"message_id": "om_m", "root_id": "om_root", "chat_id": "oc_c", "msg_type": "interactive", "body": map[string]any{"content": content}}}}})
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"chat_mode":"group"}}`))
	}))
	defer srv.Close()
	defer func() {
		select {
		case <-blocked:
		default:
			close(blocked)
		}
	}()
	h := &testInteractionHandler{events: make(chan interaction.Event, 4)}
	p.interactionHandlers = []interaction.Handler{h}
	p.threadIsolation = true
	done := make(chan *callback.CardActionTriggerResponse, 1)
	go func() { resp, _ := p.onCardAction(testCardEvent("select")); done <- resp }()
	select {
	case resp := <-done:
		if resp == nil || resp.Toast == nil {
			t.Fatal("missing toast")
		}
	case <-time.After(time.Second):
		t.Fatal("query blocked callback ACK")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("no card query")
	}
	close(blocked)
	e := waitInteraction(t, h)
	if e.ThreadID != "om_root" || !strings.Contains(e.SessionID, "om_root") {
		t.Fatalf("wrong route: %+v", e)
	}
	found := false
	for _, a := range e.Answers {
		if a.Value == "v1" && a.Label == "生产环境" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unmapped answer: %+v", e.Answers)
	}
}
func TestCardQueryFailureStillEnqueuesAnswer(t *testing.T) {
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":999,"msg":"access denied"}`))
	}))
	defer srv.Close()
	p.inbox = common.NewInbox(4)
	// Avoid unrelated user profile IO when asserting the agent payload.

	p.onCardAction(testCardEvent("select"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	msg, err := p.inbox.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(msg.Message)
	for _, s := range []string{"failed", "access denied", "v1", "om_m", "raw_callback"} {
		if !strings.Contains(string(raw), s) {
			t.Errorf("missing %s: %s", s, raw)
		}
	}
}

func TestUnresolvedThreadUsesIsolatedCardSession(t *testing.T) {
	p, h := interactionPlatform()
	p.threadIsolation = true
	p.onCardAction(testCardEvent("answer"))
	e := waitInteraction(t, h)
	if !strings.Contains(e.SessionID, "om_m") {
		t.Fatalf("unresolved topic must not enter a flat session: %+v", e)
	}
}
func TestRawCallbackRetainsUnknownSDKFields(t *testing.T) {
	ev := testCardEvent("answer")
	ev.EventReq = &larkevent.EventReq{Body: []byte(`{"event":{"action":{"name":"answer","value":{"action":"answer"},"custom_future_field":{"answer":"yes"}}}}`)}
	e := feishuInteractionSnapshot(ev, "s", "c", "m", "u", "answer")
	if !strings.Contains(string(e.RawCallback), "custom_future_field") || !strings.Contains(string(e.RawAction), "custom_future_field") {
		t.Fatal("lost raw callback fields")
	}
}

func TestPermissionCardProducesOnlyPermissionReply(t *testing.T) {
	p, h := interactionPlatform()
	p.inbox = common.NewInbox(4)
	ev := testCardEvent("perm:allow")
	ev.Event.Action.Value["request_id"] = "request1"
	ev.Event.Action.Value["conversation_id"] = "conversation1"
	resp, err := p.onCardAction(ev)
	if err != nil || resp == nil || resp.Card == nil {
		t.Fatalf("permission confirmation lost: %v %v", resp, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, err := p.inbox.Receive(ctx)
	if err != nil || len(msg.Reply) == 0 {
		t.Fatalf("missing permission reply: %+v %v", msg, err)
	}
	p.onCardAction(ev)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	if _, err = p.inbox.Receive(ctx2); err == nil {
		t.Fatal("duplicate permission reply")
	}
	select {
	case <-h.events:
		t.Fatal("permission reached plugin")
	default:
	}
}
func TestCardRestoresPrivateAndRejectsForeignSession(t *testing.T) {
	for _, tc := range []struct {
		name, mode, key string
		want            bool
	}{{"private", "p2p", "lark:ou_u:u:ou_u", true}, {"foreign_chat", "group", "lark:oc_other:u:ou_u", false}, {"foreign_user", "group", "lark:oc_c:u:ou_other", false}} {
		t.Run(tc.name, func(t *testing.T) {
			p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "/messages/") {
					_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"message_id":"om_m","chat_id":"oc_c","msg_type":"interactive","body":{"content":"{\"title\":\"question\"}"}}]}}`))
					return
				}
				_, _ = fmt.Fprintf(w, `{"code":0,"data":{"chat_mode":%q}}`, tc.mode)
			}))
			defer srv.Close()
			h := &testInteractionHandler{events: make(chan interaction.Event, 4)}
			p.interactionHandlers = []interaction.Handler{h}
			ev := testCardEvent("external")
			ev.Event.Action.Value["session_key"] = tc.key
			p.onCardAction(ev)
			e := waitInteraction(t, h)
			if (e.SessionID == tc.key) != tc.want {
				t.Fatalf("wrong session: %+v", e)
			}
		})
	}
}

func TestFeishuNestedFormValuesMapToOptionLabels(t *testing.T) {
	ev := testCardEvent("submit")
	ev.Event.Action.FormValue = map[string]any{"env": map[string]any{"value": []any{"a", "b"}}, "clear": []any{}}
	e := feishuInteractionSnapshot(ev, "s", "c", "m", "u", "submit")
	e.RawCard = json.RawMessage(`{"elements":[{"name":"env","options":[{"value":"a","text":{"content":"生产"}},{"value":"b","text":{"content":"测试"}}]}]}`)
	common.EnrichInteractionCard(&e)
	labels := map[string]bool{}
	cleared := false
	for _, a := range e.Answers {
		labels[a.Label] = true
		if a.Control == "clear" {
			cleared = true
		}
	}
	if !labels["生产"] || !labels["测试"] || !cleared {
		t.Fatalf("nested form answers lost: %+v", e.Answers)
	}
}

type cardCommandSpy struct{ calls chan string }

func (s *cardCommandSpy) Dispatch(_ context.Context, name, args string) (string, error) {
	s.calls <- name
	return "", nil
}
func (s *cardCommandSpy) List(context.Context) []agentkit.Command { return nil }
func TestNativeCardCommandKeepsRefreshTarget(t *testing.T) {
	for _, prefix := range []string{"act:", "nav:", "cmd:"} {
		t.Run(prefix, func(t *testing.T) {
			p, h := interactionPlatform()
			spy := &cardCommandSpy{calls: make(chan string, 2)}
			p.commands = spy
			p.onCardAction(testCardEvent(prefix + "/builtin"))
			select {
			case name := <-spy.calls:
				if name != "builtin" {
					t.Fatal(name)
				}
			case <-time.After(time.Second):
				t.Fatal("builtin not called")
			}
			if prefix != "cmd:" {
				p.cardActionMsgMu.Lock()
				target := p.cardActionMsgIDs[p.makeSessionKey(nil, "oc_c", "ou_u")]
				p.cardActionMsgMu.Unlock()
				if target != "om_m" {
					t.Fatal("RefreshCard target lost")
				}
			}
			select {
			case <-h.events:
				t.Fatal("builtin forwarded")
			case <-time.After(30 * time.Millisecond):
			}
		})
	}
}

func TestDistinctCardEventsPreserveRepeatedAnswer(t *testing.T) {
	p, h := interactionPlatform()
	send := func(id, option string) {
		ev := testCardEvent("custom")
		ev.Event.Action.Option = option
		body, _ := json.Marshal(map[string]any{"header": map[string]any{"event_id": id}, "event": ev.Event})
		ev.EventReq = &larkevent.EventReq{Body: body}
		p.onCardAction(ev)
	}
	send("event-1", "A")
	waitInteraction(t, h)
	send("event-2", "B")
	waitInteraction(t, h)
	send("event-3", "A")
	select {
	case <-h.events:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("third distinct callback event was suppressed: A -> B -> A")
	}
}

type profileMetadataCommands struct{ seen chan map[string]any }

func (c *profileMetadataCommands) Dispatch(ctx context.Context, _ string, _ string) (string, error) {
	c.seen <- rctx.MetadataFromContext(ctx)
	return "", nil
}
func (c *profileMetadataCommands) List(context.Context) []agentkit.Command { return nil }
func TestCardCommandPreservesProfileMetadata(t *testing.T) {
	p, _ := interactionPlatform()
	p.userProfileCache.Store("ou_u", feishuUserProfileEntry{name: "Alice", email: "alice@example.com", ok: true})
	normal := p.inboundMetadata(inboundMessage{userID: "ou_u"})
	if normal["displayName"] != "Alice" {
		t.Fatal("probe setup did not populate normal metadata")
	}
	c := &profileMetadataCommands{seen: make(chan map[string]any, 1)}
	p.commands = c
	p.onCardAction(testCardEvent("cmd:/me"))
	select {
	case md := <-c.seen:
		if md["displayName"] != "Alice" || md["email"] != "alice@example.com" {
			t.Fatalf("card command metadata lost: %#v (normal=%#v)", md, normal)
		}
	case <-time.After(time.Second):
		t.Fatal("command not dispatched")
	}
}

func TestFeishuInteractionProfileLookupHonorsContext(t *testing.T) {
	release := make(chan struct{})
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan map[string]any, 1)
	go func() { done <- p.userProfileMetadataContext(ctx, "ou_u") }()
	select {
	case md := <-done:
		if len(md) != 0 {
			t.Fatal(md)
		}
	case <-time.After(time.Second):
		t.Fatal("profile lookup ignored caller context")
	}
}

func TestDMThreadCardRecovery(t *testing.T) {
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/messages/") {
			_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"message_id":"om_m","root_id":"om_root","thread_id":"omt_topic","chat_id":"oc_c","msg_type":"interactive","body":{"content":"{}"}}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"chat_mode":"p2p"}}`))
	}))
	defer srv.Close()
	p.cfg.DMThreadMode = true
	e := feishuInteractionSnapshot(testCardEvent("answer"), "", "oc_c", "om_m", "ou_u", "answer")
	chatType, valid := p.enrichCardInteraction(context.Background(), &e)
	if !valid || chatType != "p2p" || e.ThreadID != "om_root" {
		t.Fatalf("DM card lost topic route: chatType=%s event=%+v", chatType, e)
	}
	rc := replyContext{messageID: e.MessageID, chatType: chatType, sessionKey: e.SessionID}
	if !p.shouldReplyInThread(rc) {
		t.Fatal("recovered DM card must reply in topic")
	}
}
