package feishu

import (
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestDeliveryForSendParsesSessionKey(t *testing.T) {
	t.Parallel()

	p := &Platform{platformTag: "lark"}
	rc, ok := p.deliveryForSend(agentkit.SessionID("lark:oc_d75b209bf7d268a1602fea336e9bb41e"))
	if !ok {
		t.Fatal("expected parsed delivery")
	}
	if rc.chatID != "oc_d75b209bf7d268a1602fea336e9bb41e" {
		t.Fatalf("chatID = %q", rc.chatID)
	}
	if rc.messageID != "" {
		t.Fatalf("messageID = %q, want empty for bare chat route", rc.messageID)
	}
}

func TestDeliveryForSendPrefersCachedDelivery(t *testing.T) {
	t.Parallel()

	p := &Platform{platformTag: "lark"}
	sessionID := agentkit.SessionID("lark:oc_test")
	want := replyContext{chatID: "oc_test", chatType: "group", messageID: "om_cached"}
	p.storeDelivery(sessionID, want)

	rc, ok := p.deliveryForSend(sessionID)
	if !ok {
		t.Fatal("expected cached delivery")
	}
	if rc != want {
		t.Fatalf("delivery = %+v, want %+v", rc, want)
	}
}
