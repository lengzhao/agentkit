package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit/cap/interaction"
)

func TestFeishuSubmissionPolicyDistinguishesFormChanges(t *testing.T) {
	ev := testCardEvent("choose")
	e := feishuInteractionSnapshot(ev, "s", "c", "m", "u", "choose")
	e.RawCard = json.RawMessage(`{"elements":[{"tag":"form","elements":[{"tag":"select_static","name":"answer"}]}]}`)
	normalizeFeishuInteraction(&e)
	if e.Purpose != interaction.PurposeChange {
		t.Fatalf("form edit is not draft: %s", e.Purpose)
	}
	e.RawCard = json.RawMessage(`{"elements":[{"tag":"select_static","name":"answer"}]}`)
	normalizeFeishuInteraction(&e)
	if e.Purpose != interaction.PurposeSubmit {
		t.Fatal("standalone answer did not submit")
	}
	ev.Event.Action.Value["agentkit_interaction"] = map[string]any{"purpose": "submit", "mode": "per_user"}
	e = feishuInteractionSnapshot(ev, "s", "c", "m", "u", "choose")
	if e.SubmissionMode != interaction.SubmissionPerUser {
		t.Fatal("explicit policy lost")
	}
}

func TestFeishuFallbackClosesOnceEvenIfPatchFails(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			patched := make(chan string, 2)
			p, srv := newCardkitMockPlatform(cardkitMockHandler(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPatch {
					var body struct {
						Content string `json:"content"`
					}
					_ = json.NewDecoder(r.Body).Decode(&body)
					patched <- body.Content
					if fail {
						_, _ = w.Write([]byte(`{"code":999,"msg":"patch rejected"}`))
					} else {
						_, _ = w.Write([]byte(`{"code":0}`))
					}
					return
				}
				if strings.Contains(r.URL.Path, "/messages/") {
					card := `{"header":{"title":{"content":"请选择环境"}},"elements":[{"tag":"button","name":"answer","text":{"content":"生产"},"value":{"action":"choose","answer":"prod"}}]}`
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []any{map[string]any{"message_id": "om_m", "chat_id": "oc_c", "msg_type": "interactive", "body": map[string]any{"content": card}}}}})
					return
				}
				_, _ = w.Write([]byte(`{"code":0,"data":{"chat_mode":"group"}}`))
			}))
			defer srv.Close()
			p.inbox = common.NewInbox(4)
			p.userProfileCache.Store("ou_u", feishuUserProfileEntry{})
			send := func(id string) {
				ev := testCardEvent("choose")
				ev.Event.Action.Tag = "button"
				ev.Event.Action.Option = ""
				ev.Event.Action.Value["answer"] = "prod"
				ev.EventV2Base = &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: id}}
				_, _ = p.onCardAction(ev)
			}
			send("event1")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := p.inbox.Receive(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case body := <-patched:
				if strings.Contains(body, `"tag":"button"`) || !strings.Contains(body, "生产") || !strings.Contains(body, "已提交") {
					t.Fatalf("card did not become read-only: %s", body)
				}
			case <-ctx.Done():
				t.Fatal("card not updated")
			}
			send("event2")
			noEvent, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer stop()
			if _, err := p.inbox.Receive(noEvent); err == nil {
				t.Fatal("second click triggered agent")
			}
		})
	}
}

func TestFeishuFormResetIsNotFinalSubmission(t *testing.T) {
	ev := testCardEvent("reset")
	ev.Event.Action.Tag = "button"
	e := feishuInteractionSnapshot(ev, "s", "c", "m", "u", "reset")
	if e.SubmissionMode != interaction.SubmissionRepeat {
		t.Fatal("unknown reset layout locked")
	}
	e.RawCard = json.RawMessage(`{"elements":[{"tag":"form","elements":[{"tag":"button","name":"answer","form_action_type":"reset"}]}]}`)
	normalizeFeishuInteraction(&e)
	if e.Purpose != interaction.PurposeChange {
		t.Fatal("form reset incorrectly submits")
	}
	e.RawCard = json.RawMessage(`{"elements":[{"tag":"form","elements":[{"tag":"button","name":"answer","form_action_type":"submit"}]}]}`)
	normalizeFeishuInteraction(&e)
	if e.Purpose != interaction.PurposeSubmit {
		t.Fatal("final form submit ignored")
	}
}

func TestFeishuUnknownFormContextDoesNotLock(t *testing.T) {
	e := feishuInteractionSnapshot(testCardEvent("choose"), "s", "c", "m", "u", "choose")
	if e.SubmissionMode != interaction.SubmissionRepeat {
		t.Fatal("unknown form context locked")
	}
}
