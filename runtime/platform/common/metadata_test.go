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
