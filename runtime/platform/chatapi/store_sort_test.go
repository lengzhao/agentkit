package chatapi

import (
	"testing"
	"time"
)

func TestConversationMoreRecentUsesIndexSeq(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := old.Add(time.Hour)
	a := &conversation{ID: "a", UpdatedAt: newer, indexLastSeq: 1}
	b := &conversation{ID: "b", UpdatedAt: old, indexLastSeq: 5}
	if !conversationMoreRecent(b, a) {
		t.Fatal("higher indexLastSeq should win despite older UpdatedAt")
	}
}
