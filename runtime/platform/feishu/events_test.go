package feishu

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/lengzhao/agentkit"
	caplarkevent "github.com/lengzhao/agentkit/cap/larkevent"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

// recordingHandler 记录收到的事件 event_id。
type recordingHandler struct {
	eventTypes []string
	handled    chan string
}

func (h *recordingHandler) EventTypes() []string { return h.eventTypes }

func (h *recordingHandler) Handle(_ context.Context, event caplarkevent.Event) error {
	if h.handled != nil {
		h.handled <- event.EventID
	}
	return nil
}

func TestParseCustomEvent(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantID    string
		wantType  string
		wantChat  string
		wantUser  string
		wantHasTs bool
	}{
		{
			name: "chat and sender",
			body: `{
				"schema":"2.0",
				"header":{"event_id":"e1","event_type":"im.chat.member.user.added_v1","create_time":"1700000000000"},
				"event":{"chat_id":"oc_1","sender":{"sender_id":{"open_id":"ou_1"}}}
			}`,
			wantID:    "e1",
			wantType:  "im.chat.member.user.added_v1",
			wantChat:  "oc_1",
			wantUser:  "ou_1",
			wantHasTs: true,
		},
		{
			name: "message chat_id and operator",
			body: `{
				"header":{"event_id":"e2","event_type":"im.chat.updated_v1"},
				"event":{"message":{"chat_id":"oc_2"},"operator":{"operator_id":{"open_id":"ou_2"}}}
			}`,
			wantID:   "e2",
			wantType: "im.chat.updated_v1",
			wantChat: "oc_2",
			wantUser: "ou_2",
		},
		{
			name: "no context",
			body: `{
				"header":{"event_id":"e3","event_type":"drive.file.created_v1"},
				"event":{"file_token":"x"}
			}`,
			wantID:   "e3",
			wantType: "drive.file.created_v1",
		},
		{
			name: "subscriber_ids carries user identity",
			body: `{
				"schema":"2.0",
				"header":{"event_id":"e4","event_type":"minutes.minute.generated_v1","create_time":"1792045223366"},
				"event":{"minute_token":"token1","subscriber_ids":[{"open_id":"ou_sub","user_id":"u1"}]}
			}`,
			wantID:    "e4",
			wantType:  "minutes.minute.generated_v1",
			wantUser:  "ou_sub",
			wantHasTs: true,
		},
		{
			name: "empty subscriber_ids falls back to empty user",
			body: `{
				"header":{"event_id":"e5","event_type":"vc.note.generated_v1"},
				"event":{"subscriber_ids":[]}
			}`,
			wantID:   "e5",
			wantType: "vc.note.generated_v1",
		},
		{
			name:     "invalid json falls back to registered type",
			body:     `not-json`,
			wantType: "fallback.type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := parseCustomEvent("fallback.type", []byte(tt.body))
			if evt.EventID != tt.wantID {
				t.Errorf("EventID = %q, want %q", evt.EventID, tt.wantID)
			}
			if evt.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", evt.Type, tt.wantType)
			}
			if evt.ChatID != tt.wantChat {
				t.Errorf("ChatID = %q, want %q", evt.ChatID, tt.wantChat)
			}
			if evt.UserID != tt.wantUser {
				t.Errorf("UserID = %q, want %q", evt.UserID, tt.wantUser)
			}
			if gotTs := !evt.createTime.IsZero(); gotTs != tt.wantHasTs {
				t.Errorf("createTime set = %v, want %v", gotTs, tt.wantHasTs)
			}
			if string(evt.Raw) != tt.body {
				t.Errorf("Raw should carry the original body")
			}
		})
	}
}

func TestCustomEventRoutes(t *testing.T) {
	h1 := &recordingHandler{eventTypes: []string{"a_v1", "b_v1", "  "}}
	h2 := &recordingHandler{eventTypes: []string{"a_v1"}}
	p := &Platform{
		platformTag:    "lark",
		eventHandlers:  []caplarkevent.Handler{h1, h2},
		forwardToAgent: map[string]bool{"b_v1": true, "c_v1": true},
	}
	routes := p.customEventRoutes()

	if got := len(routes["a_v1"].handlers); got != 2 {
		t.Errorf("a_v1 handlers = %d, want 2", got)
	}
	if routes["a_v1"].toAgent {
		t.Errorf("a_v1 toAgent = true, want false")
	}
	if got := len(routes["b_v1"].handlers); got != 1 || !routes["b_v1"].toAgent {
		t.Errorf("b_v1 = handlers %d toAgent %v, want 1 + true", got, routes["b_v1"].toAgent)
	}
	if got := len(routes["c_v1"].handlers); got != 0 || !routes["c_v1"].toAgent {
		t.Errorf("c_v1 = handlers %d toAgent %v, want 0 + true", got, routes["c_v1"].toAgent)
	}
	if _, ok := routes[""]; ok {
		t.Errorf("blank event type must be skipped")
	}
}

func TestDispatchCustomEventDedup(t *testing.T) {
	h := &recordingHandler{eventTypes: []string{"a_v1"}, handled: make(chan string, 2)}
	p := &Platform{
		platformTag: "lark",
		inbox:       common.NewInbox(4),
		dedup:       &common.MessageDedup{},
	}
	route := &customEventRoute{handlers: []caplarkevent.Handler{h}}
	body := []byte(`{"header":{"event_id":"dup-1","event_type":"a_v1"},"event":{}}`)
	req := &larkevent.EventReq{Body: body}

	p.dispatchCustomEvent("a_v1", route, req)
	p.dispatchCustomEvent("a_v1", route, req) // 同 event_id，应被去重

	select {
	case id := <-h.handled:
		if id != "dup-1" {
			t.Fatalf("handled event_id = %q, want dup-1", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not called")
	}
	select {
	case <-h.handled:
		t.Fatal("duplicate event reached handler")
	case <-time.After(200 * time.Millisecond):
	}
}

// 重启前的旧事件（飞书重推补送）不应被进程启动时间过滤丢弃——
// 停机期间的事件正是靠重推送达，重复投递由 event_id 去重兜底。
func TestDispatchCustomEventAcceptsPreStartEvent(t *testing.T) {
	h := &recordingHandler{eventTypes: []string{"a_v1"}, handled: make(chan string, 1)}
	p := &Platform{
		platformTag: "lark",
		inbox:       common.NewInbox(4),
		dedup:       &common.MessageDedup{},
	}
	route := &customEventRoute{handlers: []caplarkevent.Handler{h}}
	// create_time 为 1 小时前，远早于进程启动时间
	body := []byte(`{"header":{"event_id":"old-1","event_type":"a_v1","create_time":"1700000000000"},"event":{}}`)
	p.dispatchCustomEvent("a_v1", route, &larkevent.EventReq{Body: body})

	select {
	case id := <-h.handled:
		if id != "old-1" {
			t.Fatalf("handled event_id = %q, want old-1", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pre-start event was dropped; retries after downtime must be accepted")
	}
}

func TestForwardEventToAgentRouting(t *testing.T) {
	tests := []struct {
		name         string
		chatID       string
		userID       string
		wantSession  string
		wantRcChat   string
		wantChatType string
	}{
		{name: "group context", chatID: "oc_1", userID: "ou_1", wantSession: "lark:oc_1:u:ou_1", wantRcChat: "oc_1", wantChatType: "group"},
		{name: "user only", userID: "ou_1", wantSession: "lark:ou_1:u:ou_1", wantRcChat: "ou_1", wantChatType: "p2p"},
		{name: "no context", wantSession: "lark:events", wantRcChat: "", wantChatType: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Platform{
				platformTag: "lark",
				inbox:       common.NewInbox(4),
				dedup:       &common.MessageDedup{},
			}
			raw, _ := json.Marshal(map[string]any{"event_type": "x_v1"})
			p.forwardEventToAgent(customEvent{
				Event: caplarkevent.Event{Type: "x_v1", EventID: "e1", ChatID: tt.chatID, UserID: tt.userID, Raw: raw},
			})

			rc, ok := p.deliveryFor(agentkit.SessionID(tt.wantSession))
			if !ok {
				t.Fatalf("no delivery stored for session %q", tt.wantSession)
			}
			if rc.chatID != tt.wantRcChat {
				t.Errorf("reply chatID = %q, want %q", rc.chatID, tt.wantRcChat)
			}
			if rc.chatType != tt.wantChatType {
				t.Errorf("reply chatType = %q, want %q", rc.chatType, tt.wantChatType)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			event, err := p.inbox.Receive(ctx)
			if err != nil {
				t.Fatalf("inbox receive: %v", err)
			}
			text := assistantText(event.Message)
			if !strings.Contains(text, "[事件] x_v1") {
				t.Errorf("inbound content = %q, want [事件] x_v1 prefix", text)
			}
		})
	}
}

func TestReceiveIDType(t *testing.T) {
	if got := receiveIDType("ou_abc"); got != "open_id" {
		t.Errorf("receiveIDType(ou_abc) = %q, want open_id", got)
	}
	if got := receiveIDType("oc_abc"); got != "chat_id" {
		t.Errorf("receiveIDType(oc_abc) = %q, want chat_id", got)
	}
}
