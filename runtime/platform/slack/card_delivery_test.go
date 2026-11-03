package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestPluginCardBeforeReceive(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.ParseForm()
		if r.URL.Path != "/api/chat.postMessage" || r.Form.Get("channel") != "C123" || !strings.Contains(r.Form.Get("blocks"), "approve") {
			t.Errorf("card request=%s %v", r.URL.Path, r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"channel":"C123","ts":"100.1"}`)
	}))
	defer srv.Close()
	platform, err := New(Config{BotToken: "bot", AppToken: "app", Domain: srv.URL + "/"}, Deps{Workspace: rtworkspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	sender := platform.(cap.CardSender)
	card := cap.NewCard().Title("Task", "blue").Markdown("Ready").Build()
	card.Elements = append(card.Elements, cap.CardActions{Buttons: []cap.CardButton{{Text: "Approve", Value: "approve"}}})
	ref, err := sender.SendCard(context.Background(), rctx.SessionRoute("slack", "slack:C123"), card)
	if err != nil || ref.ID != "100.1" || ref.Route.Instance != "slack" || calls != 1 {
		t.Fatalf("%+v %v calls=%d", ref, err, calls)
	}
	var saved cap.MessageRef
	raw, _ := json.Marshal(ref)
	if err = json.Unmarshal(raw, &saved); err != nil || saved.ID != ref.ID {
		t.Fatal("reference cannot be persisted")
	}
	if _, err = sender.SendCard(context.Background(), rctx.SessionRoute("slack", "slack:C123"), nil); err == nil || calls != 1 {
		t.Fatal("nil card sent")
	}
}
func TestPluginCardUnknownIsNotRetried(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(502) }))
	defer srv.Close()
	platform, err := New(Config{BotToken: "bot", AppToken: "app", Domain: srv.URL + "/"}, Deps{Workspace: rtworkspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	_, err = platform.(cap.CardSender).SendCard(context.Background(), rctx.SessionRoute("slack", "slack:C123"), cap.NewCard().Markdown("card").Build())
	var de *cap.Error
	if !errors.As(err, &de) || !de.Unknown || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
