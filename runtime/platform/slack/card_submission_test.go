package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit/cap/interaction"
	slackapi "github.com/slack-go/slack"
)

func TestSlackSubmissionPolicyDistinguishesDrafts(t *testing.T) {
	var cb slackapi.InteractionCallback
	_ = json.Unmarshal([]byte(`{"type":"block_actions","view":{"id":"v"}}`), &cb)
	e := slackInteractionSnapshot(cb, &slackapi.BlockAction{ActionID: "env", Type: "static_select"}, "s", "c", "", "", "prod")
	if e.Purpose != interaction.PurposeChange {
		t.Fatal("modal edit submitted")
	}
	cb.Type = slackapi.InteractionTypeViewSubmission
	e = slackInteractionSnapshot(cb, nil, "s", "c", "", "", "submit")
	if e.Purpose != interaction.PurposeSubmit {
		t.Fatal("modal submit became draft")
	}
	action, session, _ := decodeActionValue(`{"action":"submit","session_key":"s","agentkit_interaction":{"mode":"per_user"}}`)
	if action != "submit" || session != "s" {
		t.Fatal("nested policy broke action decoding")
	}
}

func TestSlackFallbackClosesCardOnce(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			updates := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "chat.postEphemeral") {
					_, _ = w.Write([]byte(`{"ok":true,"message_ts":"2"}`))
					return
				}
				if !strings.HasSuffix(r.URL.Path, "chat.update") {
					t.Errorf("unexpected API: %s", r.URL.Path)
				}
				_ = r.ParseForm()
				body := []byte(r.Form.Get("blocks"))
				if strings.Contains(string(body), `"type":"button"`) || !strings.Contains(string(body), "已提交") {
					t.Errorf("interactive card remained: %s", body)
				}
				updates++
				if fail {
					_, _ = w.Write([]byte(`{"ok":false,"error":"cant_update_message"}`))
				} else {
					_, _ = w.Write([]byte(`{"ok":true,"channel":"C1","ts":"1"}`))
				}
			}))
			defer srv.Close()
			p := &Platform{client: slackapi.New("token", slackapi.OptionAPIURL(srv.URL+"/")), inbox: common.NewInbox(4)}
			p.userProfileCache.Store("U1", slackUserProfileEntry{})
			var cb slackapi.InteractionCallback
			_ = json.Unmarshal([]byte(`{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"1","text":"请选择环境"}}`), &cb)
			e := slackInteractionSnapshot(cb, &slackapi.BlockAction{ActionID: "answer", Type: "button", Value: "prod"}, p.buildSessionKey("C1", "U1", ""), "C1", "1", "", "prod")
			for i := 0; i < 2; i++ {
				if err := p.processBlockInteraction(context.Background(), e); err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					_, err := p.inbox.Receive(ctx)
					cancel()
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if updates != 1 {
				t.Fatalf("card updated %d times", updates)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			if _, err := p.inbox.Receive(ctx); err == nil {
				t.Fatal("duplicate triggered agent")
			}
		})
	}
}

func TestSlackPerUserReceiptDoesNotCloseSharedCard(t *testing.T) {
	receipts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "chat.postEphemeral") {
			t.Errorf("shared card was modified: %s", r.URL.Path)
		}
		receipts++
		_, _ = w.Write([]byte(`{"ok":true,"message_ts":"2"}`))
	}))
	defer srv.Close()
	p := &Platform{client: slackapi.New("token", slackapi.OptionAPIURL(srv.URL+"/")), inbox: common.NewInbox(4)}
	var cb slackapi.InteractionCallback
	_ = json.Unmarshal([]byte(`{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C1"},"message":{"ts":"1","text":"question"}}`), &cb)
	e := slackInteractionSnapshot(cb, &slackapi.BlockAction{ActionID: "answer", Type: "button", Value: `{"action":"prod","agentkit_interaction":{"mode":"per_user"}}`}, "s", "C1", "1", "", "prod")
	for _, user := range []string{"U1", "U1", "U2"} {
		e.UserID = user
		e.SessionID = p.buildSessionKey("C1", user, "")
		p.userProfileCache.Store(user, slackUserProfileEntry{})
		if err := p.processBlockInteraction(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	if receipts != 3 {
		t.Fatalf("receipts=%d", receipts)
	}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := p.inbox.Receive(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := p.inbox.Receive(ctx); err == nil {
		t.Fatal("per-user duplicate forwarded")
	}
}

func TestSlackSelectedOptionPolicyAndUnknownFormContext(t *testing.T) {
	var cb slackapi.InteractionCallback
	_ = json.Unmarshal([]byte(`{"type":"block_actions"}`), &cb)
	a := &slackapi.BlockAction{ActionID: "answer", Type: "static_select"}
	e := slackInteractionSnapshot(cb, a, "s", "c", "m", "", "choose")
	if e.SubmissionMode != interaction.SubmissionRepeat {
		t.Fatal("unknown form context locked")
	}
	a.SelectedOption.Value = `{"agentkit_interaction":{"purpose":"submit","mode":"per_user"}}`
	e = slackInteractionSnapshot(cb, a, "s", "c", "m", "", "choose")
	if e.SubmissionMode != interaction.SubmissionPerUser {
		t.Fatal("selected option policy ignored")
	}
}
