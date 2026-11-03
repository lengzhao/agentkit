package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func businessRoute() agentkit.RouteRef {
	return rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", Instance: "business", ChannelID: "oc_target"})
}
func TestBusinessMessagesPublishReadEdit(t *testing.T) {
	calls := 0
	var uuids []string
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case "POST":
			calls++
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["receive_id"] != "oc_target" || body["msg_type"] != "post" {
				t.Errorf("body=%v", body)
			}
			content := body["content"].(string)
			if calls == 1 && (!strings.Contains(content, `"tag":"at"`) || !strings.Contains(content, "ou_target")) {
				t.Errorf("missing mention: %s", content)
			}
			uuids = append(uuids, body["uuid"].(string))
			fmt.Fprintf(w, `{"code":0,"data":{"message_id":"om_%d","chat_id":"oc_target"}}`, calls)
		case "GET":
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"message_id":"om_1","chat_id":"oc_target","msg_type":"text","body":{"content":"{\"text\":\"restored\"}"}}]}}`)
		case "PUT":
			fmt.Fprint(w, `{"code":0,"data":{}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	p.SetPlatformInstance("business")
	req := cap.PublishRequest{Route: businessRoute(), OperationID: "stable", Content: []cap.Part{{Type: "mention", UserID: "ou_target"}, {Type: "text", Text: strings.Repeat("中", 3100)}}}
	receipt, err := p.Publish(context.Background(), req)
	if err != nil || len(receipt.Messages) != 2 || receipt.Route.Instance != "business" || receipt.Route.Target == nil {
		t.Fatalf("%+v %v", receipt, err)
	}
	if uuids[0] == uuids[1] || receipt.Deduplication.Window == 0 {
		t.Fatalf("bad dedup: %v %+v", uuids, receipt)
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
	ref.Route.Instance = "other"
	if _, err = p.ReadMessage(context.Background(), cap.ReadRequest{Message: ref}); err == nil {
		t.Fatal("wrong instance accepted")
	}
}
func TestBusinessMessagesPartialUnknownNoFallback(t *testing.T) {
	calls := 0
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_1","chat_id":"oc_target"}}`)
		} else {
			w.WriteHeader(502)
			fmt.Fprint(w, "unavailable")
		}
	}))
	defer srv.Close()
	p.SetPlatformInstance("business")
	receipt, err := p.Publish(context.Background(), cap.PublishRequest{Route: businessRoute(), Content: []cap.Part{{Type: "text", Text: strings.Repeat("x", 3100)}}})
	var de *cap.Error
	if !errors.As(err, &de) || de.Outcome != cap.OutcomeUnknown || len(receipt.Messages) != 1 || calls != 2 {
		t.Fatalf("%+v %v calls=%d", receipt, err, calls)
	}
}
func TestBusinessMessagesTopicIsExplicit(t *testing.T) {
	calls := 0
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_root","chat_id":"oc_target"}}`)
	}))
	defer srv.Close()
	p.SetPlatformInstance("business")
	receipt, err := p.CreateTopic(context.Background(), cap.PublishRequest{Route: businessRoute(), Content: []cap.Part{{Type: "text", Text: "root"}}})
	target, _ := rctx.DecodeSessionRoute(receipt.Route)
	if err != nil || target.ThreadID != "om_root" || calls != 1 {
		t.Fatalf("%+v %v", receipt, err)
	}
}

func TestBusinessMessagesRejectForeignThreadBeforeWrite(t *testing.T) {
	writes := 0
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"message_id":"om_foreign","chat_id":"oc_forbidden","msg_type":"text","body":{"content":"{\"text\":\"foreign\"}"}}]}}`)
		} else {
			writes++
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_written","chat_id":"oc_forbidden"}}`)
		}
	}))
	defer srv.Close()
	p.SetPlatformInstance("business")
	p.allowChat = "oc_target"
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", Instance: "business", ChannelID: "oc_target", ThreadID: "om_foreign"})
	receipt, err := p.Publish(context.Background(), cap.PublishRequest{Route: route, Content: []cap.Part{{Type: "text", Text: "private"}}})
	if err == nil || writes != 0 || receipt.Outcome != cap.OutcomeNotSent {
		t.Fatalf("writes=%d receipt=%+v err=%v", writes, receipt, err)
	}
}

func TestBusinessMessagesNormalizeThreadRootAndKeepRetryKeys(t *testing.T) {
	var keys []string
	writes := 0
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"message_id":"om_child","root_id":"om_root","chat_id":"oc_target","msg_type":"text","body":{"content":"{\"text\":\"child\"}"}}]}}`)
			return
		}
		if r.URL.Path != "/open-apis/im/v1/messages/om_root/reply" {
			t.Errorf("must use verified root: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		keys = append(keys, body["uuid"].(string))
		writes++
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_published","root_id":"om_root","chat_id":"oc_target"}}`)
	}))
	defer srv.Close()
	p.SetPlatformInstance("business")
	req := cap.PublishRequest{Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", Instance: "business", ChannelID: "oc_target", ThreadID: "om_child"}), OperationID: "retry", Caller: "consumer", Content: []cap.Part{{Type: "text", Text: "hello"}}}
	for range 2 {
		receipt, err := p.Publish(context.Background(), req)
		target, _ := rctx.DecodeSessionRoute(receipt.Route)
		if err != nil || target.ThreadID != "om_root" || len(receipt.Messages) != 1 {
			t.Fatalf("%+v %v", receipt, err)
		}
	}
	if writes != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("retry must use same native dedup key: %v", keys)
	}
}

func TestBusinessMessagesEditPreflightFailureIsNotSent(t *testing.T) {
	writes := 0
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		w.WriteHeader(502)
	}))
	defer srv.Close()
	p.SetPlatformInstance("business")
	err := p.EditMessage(context.Background(), cap.EditRequest{Message: cap.MessageRef{Route: businessRoute(), ID: "om_1"}, Content: []cap.Part{{Type: "text", Text: "update"}}})
	var de *cap.Error
	if !errors.As(err, &de) || de.Outcome != cap.OutcomeNotSent || writes != 0 {
		t.Fatalf("writes=%d err=%+v", writes, de)
	}
}
