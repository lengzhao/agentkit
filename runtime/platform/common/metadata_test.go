package common

import (
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestUserProfileMetadata(t *testing.T) {
	if md := UserProfileMetadata("", ""); md != nil {
		t.Fatalf("got %v", md)
	}
	md := UserProfileMetadata("Alice", "alice@example.com")
	if md["displayName"] != "Alice" || md["email"] != "alice@example.com" {
		t.Fatalf("got %v", md)
	}
}

func TestWithMetadataPreservesExistingKeys(t *testing.T) {
	event := WithMetadata(agentkit.MessageEvent{
		Metadata: map[string]any{"displayName": "Bob"},
	}, map[string]any{
		"displayName": "Alice",
		"email":       "alice@example.com",
	})
	if event.Metadata["displayName"] != "Bob" {
		t.Fatalf("displayName = %v", event.Metadata["displayName"])
	}
	if event.Metadata["email"] != "alice@example.com" {
		t.Fatalf("email = %v", event.Metadata["email"])
	}
}

func TestMentionProfilesMetadata(t *testing.T) {
	t.Parallel()
	if md := MentionProfilesMetadata(nil); md != nil {
		t.Fatalf("got %v", md)
	}
	md := MentionProfilesMetadata([]MentionProfile{
		{ID: "ou_1", Name: "Alice", Email: "alice@example.com"},
		{Name: "Bob"},
	})
	items, ok := md["mentions"].([]map[string]string)
	if !ok || len(items) != 2 {
		t.Fatalf("mentions = %#v", md["mentions"])
	}
}

func TestMergeMetadataPreservesExistingKeys(t *testing.T) {
	t.Parallel()
	got := MergeMetadata(map[string]any{"displayName": "Alice"}, map[string]any{
		"displayName": "Bob",
		"mentions":    []map[string]string{{"id": "ou_1"}},
	})
	if got["displayName"] != "Alice" {
		t.Fatalf("displayName = %v", got["displayName"])
	}
	if got["mentions"] == nil {
		t.Fatalf("mentions missing: %v", got)
	}
}
