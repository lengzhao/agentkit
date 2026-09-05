package common

import (
	"testing"

	"github.com/lengzhao/agentkit/cap/permission"
)

func TestPermissionCardFromQuestionPayload(t *testing.T) {
	card := PermissionCardFromPayload(permission.RequestPayload{
		Request: permission.Request{
			ID:   "permabc",
			Kind: permission.KindQuestion,
			Question: &permission.Question{
				Prompt: "Pick one",
				Options: []permission.Option{
					{Label: "A"},
					{Label: "B"},
				},
			},
		},
	})
	if card == nil || card.Header == nil {
		t.Fatal("expected card header")
	}
	if len(card.Elements) < 2 {
		t.Fatalf("elements = %d", len(card.Elements))
	}
	item, ok := card.Elements[1].(CardListItem)
	if !ok {
		t.Fatalf("element type %T", card.Elements[1])
	}
	if item.Extra["request_id"] != "permabc" || item.Extra["answer_text"] != "A" || item.Extra["selected"] != "0" {
		t.Fatalf("extra = %v", item.Extra)
	}
}

func TestPermissionCardFromQuestionPayloadIncludesConversation(t *testing.T) {
	card := PermissionCardFromPayload(permission.RequestPayload{
		Request: permission.Request{
			ID:   "permabc",
			Kind: permission.KindQuestion,
			Question: &permission.Question{
				Prompt:  "Pick one",
				Options: []permission.Option{{Label: "A"}},
			},
		},
		Conversation: "lark:oc_test:new:20260101",
	})
	item, ok := card.Elements[1].(CardListItem)
	if !ok {
		t.Fatalf("element type %T", card.Elements[1])
	}
	if item.Extra["conversation_id"] != "lark:oc_test:new:20260101" {
		t.Fatalf("conversation_id = %q", item.Extra["conversation_id"])
	}
}

func TestPermissionCardFromAllowDenyPayload(t *testing.T) {
	card := PermissionCardFromPayload(permission.RequestPayload{
		Request: permission.Request{
			ID:     "permtool",
			Kind:   permission.KindAllowDeny,
			Reason: "run shell",
		},
	})
	if card == nil {
		t.Fatal("expected card")
	}
	if len(card.Elements) < 2 {
		t.Fatalf("elements = %d", len(card.Elements))
	}
	allow, ok := card.Elements[1].(CardListItem)
	if !ok {
		t.Fatalf("element type %T", card.Elements[1])
	}
	if allow.Extra["request_id"] != "permtool" || allow.Extra["decision"] != "allow" {
		t.Fatalf("extra = %v", allow.Extra)
	}
}

func TestPermissionFieldsFromAction(t *testing.T) {
	reply, ok := PermissionReplyFromAction("perm:0", "user1", map[string]string{
		"request_id":      "abc",
		"answer_text":     "yes",
		"selected":        "0",
		"conversation_id": "lark:oc_test:new:20260101",
	})
	if !ok || reply.RequestID != "abc" || reply.UserID != "user1" || reply.Text != "yes" || len(reply.Selected) != 1 || reply.Selected[0] != 0 {
		t.Fatalf("reply = %+v ok=%v", reply, ok)
	}
	if got := PermissionConversationFromExtra(map[string]string{"conversation_id": "lark:oc_test:new:20260101"}); got != "lark:oc_test:new:20260101" {
		t.Fatalf("conversation = %q", got)
	}

	reply, ok = PermissionReplyFromAction("perm:allow", "user1", map[string]string{
		"request_id": "abc",
		"decision":   "allow",
	})
	if !ok || reply.Decision != "allow" {
		t.Fatalf("reply = %+v ok=%v", reply, ok)
	}
}

func TestPermissionCallbackData(t *testing.T) {
	data := PermissionCallbackData("req1", 2)
	requestID, index, ok := ParsePermissionCallback(data)
	if !ok || requestID != "req1" || index != 2 {
		t.Fatalf("got %q %d %v", requestID, index, ok)
	}
}

func TestConfirmedPermissionCardIsStatic(t *testing.T) {
	card := ConfirmedPermissionCard("Pick one", "A")
	if card == nil || !card.Static {
		t.Fatal("expected static confirmed card")
	}
	for _, elem := range card.Elements {
		if _, ok := elem.(CardListItem); ok {
			t.Fatal("confirmed card must not contain buttons")
		}
	}
}

func TestConfirmedCardFromAllowDenyReply(t *testing.T) {
	card := ConfirmedCardFromReply(permission.Reply{
		RequestID: "req1",
		Decision:  "allow",
	}, map[string]string{
		"perm_label": "✅ 允许",
		"perm_body":  "run shell",
		"perm_color": "green",
	})
	if card == nil || !card.Static {
		t.Fatal("expected static allow card")
	}
	if card.Header == nil || card.Header.Title != "✅ 允许" {
		t.Fatalf("header = %+v", card.Header)
	}
}

func TestLegacyInteractionCardFromPayloadRemoved(t *testing.T) {
	card := PermissionCardFromPayload(permission.RequestPayload{
		Request: permission.Request{
			ID:   "permabc",
			Kind: permission.KindQuestion,
			Question: &permission.Question{
				Prompt: "Pick one",
				Options: []permission.Option{
					{Label: "A"},
					{Label: "B"},
				},
			},
		},
	})
	if card == nil || card.Header == nil {
		t.Fatal("expected card header")
	}
	if len(card.Elements) < 2 {
		t.Fatalf("elements = %d", len(card.Elements))
	}
	item, ok := card.Elements[1].(CardListItem)
	if !ok {
		t.Fatalf("element type %T", card.Elements[1])
	}
	if _, ok := item.Extra["ix_id"]; ok {
		t.Fatalf("legacy ix_id should not be present: %v", item.Extra)
	}
}
