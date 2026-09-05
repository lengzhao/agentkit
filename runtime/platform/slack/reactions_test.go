package slack

import (
	"context"
	"testing"
)

func TestRemoveReactionNoClient(t *testing.T) {
	p := &Platform{}
	p.removeReaction(context.Background(), delivery{channel: "C1", msgTS: "1.0"}, reactionReceived)
}

func TestReactDoneMissingDelivery(t *testing.T) {
	p := &Platform{}
	p.reactDone(context.Background(), delivery{sessionID: "missing"}.sessionID)
}
