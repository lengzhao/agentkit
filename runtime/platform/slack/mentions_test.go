package slack

import (
	"testing"
)

func TestParseSlackUserMentions(t *testing.T) {
	t.Parallel()
	got := parseSlackUserMentions("hey <@U111> and <@U222|Alice> please join")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].id != "U111" || got[0].name != "" {
		t.Fatalf("first = %#v", got[0])
	}
	if got[1].id != "U222" || got[1].name != "Alice" {
		t.Fatalf("second = %#v", got[1])
	}
	if len(parseSlackUserMentions("<!here> <@U111>")) != 1 {
		t.Fatal("expected only user mention")
	}
}

func TestMentionMetadataSkipsBotAndUsesDisplayName(t *testing.T) {
	t.Parallel()
	p := &Platform{botUserID: "UBOT"}
	p.userProfileCache.Store("UALICE", slackUserProfileEntry{
		ok:    true,
		name:  "Alice Profile",
		email: "alice@example.com",
	})

	md := p.mentionMetadata("please <@UBOT> and <@UALICE|Alice Label>")
	items, ok := md["mentions"].([]map[string]string)
	if !ok {
		t.Fatalf("mentions = %#v", md["mentions"])
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0]["id"] != "UALICE" || items[0]["name"] != "Alice Profile" || items[0]["email"] != "alice@example.com" {
		t.Fatalf("item = %#v", items[0])
	}
}

func TestMentionMetadataFallsBackToMentionLabel(t *testing.T) {
	t.Parallel()
	p := &Platform{}
	md := p.mentionMetadata("cc <@UBOB|Bob Label>")
	items, ok := md["mentions"].([]map[string]string)
	if !ok || len(items) != 1 {
		t.Fatalf("mentions = %#v", md["mentions"])
	}
	if items[0]["id"] != "UBOB" || items[0]["name"] != "Bob Label" {
		t.Fatalf("item = %#v", items[0])
	}
}
