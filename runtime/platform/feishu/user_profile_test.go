package feishu

import (
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestFeishuContactUserIDType(t *testing.T) {
	if feishuContactUserIDType("ou_abc") != "open_id" {
		t.Fatal("open_id")
	}
	if feishuContactUserIDType("on_abc") != "union_id" {
		t.Fatal("union_id")
	}
	if feishuContactUserIDType("68a1aeg3") != "user_id" {
		t.Fatal("user_id")
	}
}

func TestFeishuProfileEntryPopulated(t *testing.T) {
	if feishuProfileEntryPopulated(feishuUserProfileEntry{ok: true}) {
		t.Fatal("empty profile should not populate")
	}
	if !feishuProfileEntryPopulated(feishuUserProfileEntry{ok: true, name: "A"}) {
		t.Fatal("name should populate")
	}
}

func TestEnrichInboundActorFromProfile(t *testing.T) {
	ev := enrichInboundActorFromProfile(agentkit.MessageEvent{
		UserID: "ou_x",
		Metadata: map[string]any{
			"displayName": "Alice",
			"email":       "a@b.io",
		},
	})
	if ev.Envelope.Actor.Name != "Alice" || ev.Envelope.Actor.Email != "a@b.io" {
		t.Fatalf("actor %+v", ev.Envelope.Actor)
	}
}
