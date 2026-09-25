package slack

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

func TestRemoveReactionNoClient(t *testing.T) {
	p := &Platform{}
	p.removeReaction(context.Background(), delivery{channel: "C1", msgTS: "1.0"}, reactionReceived)
}

func TestApplyTurnEndReactionsMissingDelivery(t *testing.T) {
	p := &Platform{doneEmoji: reactionDone}
	p.applyTurnEndReactions(context.Background(), agentkit.SessionID("missing"), capsession.TurnEndData{Steps: 1})
}
