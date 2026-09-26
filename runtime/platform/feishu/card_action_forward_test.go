package feishu

import (
	"strings"
	"testing"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestFormatUnknownCardActionMessage_withCardAndForm(t *testing.T) {
	action := &callback.CallBackAction{
		Name:  "submit_btn",
		Tag:   "button",
		Value: map[string]interface{}{"plan": "A"},
		FormValue: map[string]interface{}{
			"env": "prod",
		},
	}
	got := formatUnknownCardActionMessage("标题行\n正文", true, action, "confirm", "ou_user1")
	if !strings.Contains(got, "[card_action]") || !strings.Contains(got, "[/card_action]") {
		t.Fatalf("missing markers: %q", got)
	}
	if !strings.Contains(got, "标题行") || !strings.Contains(got, "action=confirm") {
		t.Fatalf("missing card or action: %q", got)
	}
	if !strings.Contains(got, "env=prod") || !strings.Contains(got, "ou_user1") {
		t.Fatalf("missing form or user: %q", got)
	}
}

func TestFormatUnknownCardActionMessage_fetchFailed(t *testing.T) {
	got := formatUnknownCardActionMessage("", false, nil, "opt_1", "ou_x")
	if strings.Contains(got, "卡片内容:") {
		t.Fatalf("should omit card section when not fetched: %q", got)
	}
	if !strings.Contains(got, "action=opt_1") {
		t.Fatalf("expected action only: %q", got)
	}
}


func TestCardActionDedupKey_stable(t *testing.T) {
	k1 := cardActionDedupKey("m1", "btn", "go", map[string]interface{}{"a": "1"})
	k2 := cardActionDedupKey("m1", "btn", "go", map[string]interface{}{"a": "1"})
	if k1 != k2 {
		t.Fatalf("keys differ: %q vs %q", k1, k2)
	}
}
