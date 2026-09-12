package learning

import (
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestShouldRunReview(t *testing.T) {
	if shouldRunReview(nil, true) {
		t.Fatal("nil messages")
	}
	msgs := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "/learn help"}}},
	}
	if shouldRunReview(msgs, true) {
		t.Fatal("slash-only should skip")
	}
	msgs = append(msgs, agentkit.ModelMessage{
		Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "remember I like tea"}},
	})
	if !shouldRunReview(msgs, true) {
		t.Fatal("expected review")
	}
}

func TestBackgroundReviewEnabledDefault(t *testing.T) {
	if !backgroundReviewEnabled(BackgroundReviewConfig{}) {
		t.Fatal("enabled should default true")
	}
	off := false
	if backgroundReviewEnabled(BackgroundReviewConfig{Enabled: &off}) {
		t.Fatal("expected disabled")
	}
}
