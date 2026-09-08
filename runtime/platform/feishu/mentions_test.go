package feishu

import (
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestMentionMetadataSkipsBotAndDedupes(t *testing.T) {
	t.Parallel()
	p := &Platform{botOpenID: "ou_bot"}
	mentions := []*larkim.MentionEvent{
		{
			Key:  strPtr("@_user_1"),
			Name: strPtr("Bot"),
			Id:   &larkim.UserId{OpenId: strPtr("ou_bot")},
		},
		{
			Key:  strPtr("@_user_2"),
			Name: strPtr("Alice"),
			Id:   &larkim.UserId{OpenId: strPtr("ou_alice")},
		},
		{
			Key:  strPtr("@_user_3"),
			Name: strPtr("Alice Again"),
			Id:   &larkim.UserId{OpenId: strPtr("ou_alice")},
		},
	}
	p.userProfileCache.Store("ou_alice", feishuUserProfileEntry{
		ok:    true,
		name:  "Alice",
		email: "alice@example.com",
	})

	md := p.mentionMetadata(mentions)
	items, ok := md["mentions"].([]map[string]string)
	if !ok {
		t.Fatalf("mentions = %#v", md["mentions"])
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0]["id"] != "ou_alice" || items[0]["name"] != "Alice" || items[0]["email"] != "alice@example.com" {
		t.Fatalf("item = %#v", items[0])
	}
}

func strPtr(s string) *string { return &s }
