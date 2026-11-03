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

func TestPluginCardDelivery(t *testing.T) {
	calls := 0
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["msg_type"] != "interactive" || body["receive_id"] != "oc_target" || !strings.Contains(body["content"].(string), "approve") {
			t.Errorf("card body=%v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":0,"data":{"message_id":"om_card","chat_id":"oc_target"}}`)
	}))
	defer srv.Close()
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", ChannelID: "oc_target"})
	card := cap.NewCard().Title("Task", "blue").Markdown("Ready").Build()
	card.Elements = append(card.Elements, cap.CardActions{Buttons: []cap.CardButton{{Text: "Approve", Value: "approve"}}})
	ref, err := p.SendCard(context.Background(), route, card)
	if err != nil || ref.ID != "om_card" || ref.Route.Instance != "lark" || calls != 1 {
		t.Fatalf("ref=%+v err=%v calls=%d", ref, err, calls)
	}
	raw, _ := json.Marshal(ref)
	var saved cap.MessageRef
	if err = json.Unmarshal(raw, &saved); err != nil || saved.ID != ref.ID {
		t.Fatalf("persist reference: %v", err)
	}
	p.allowChat = "oc_allowed"
	if _, err = p.SendCard(context.Background(), route, card); err == nil || calls != 1 {
		t.Fatal("allowlist bypassed")
	}
}
func TestPluginCardDeliveryUnknownIsNotRetried(t *testing.T) {
	calls := 0
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(502) }))
	defer srv.Close()
	_, err := p.SendCard(context.Background(), rctx.SessionRoute("lark", "lark:oc_target"), cap.NewCard().Markdown("card").Build())
	var de *cap.Error
	if !errors.As(err, &de) || !de.Unknown || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
func TestPluginCardRejectsForeignThreadBeforeSend(t *testing.T) {
	writes := 0
	p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			fmt.Fprint(w, `{"code":0,"data":{"items":[{"message_id":"om_foreign","chat_id":"oc_other"}]}}`)
		} else {
			writes++
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"sent","chat_id":"oc_other"}}`)
		}
	}))
	defer srv.Close()
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "lark", ChannelID: "oc_target", ThreadID: "om_foreign"})
	if _, err := p.SendCard(context.Background(), route, cap.NewCard().Markdown("private").Build()); err == nil || writes != 0 {
		t.Fatalf("writes=%d err=%v", writes, err)
	}
}
