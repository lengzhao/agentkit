package slack

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

func TestTurnEndReactionsIncludeSteeredMessages(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-steer")

	trigger := delivery{channel: "C1", msgTS: "1.0", sessionID: sessionID}
	steered := delivery{channel: "C1", msgTS: "2.0", sessionID: sessionID}
	p.deliveries.Store(sessionID, trigger)

	p.onTurnStartReactions(sessionID)
	p.appendTurnReactionIfActive(sessionID, steered)

	deliveries := p.finishTurnReactions(sessionID)
	if len(deliveries) != 2 {
		t.Fatalf("expected trigger + steered, got %+v", deliveries)
	}
	if deliveries[0].msgTS != "1.0" || deliveries[1].msgTS != "2.0" {
		t.Fatalf("unexpected order or ts: %+v", deliveries)
	}
}

func TestAppendTurnReactionIgnoredWhenTurnInactive(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-idle")
	late := delivery{channel: "C1", msgTS: "9.0", sessionID: sessionID}

	p.appendTurnReactionIfActive(sessionID, late)

	deliveries := p.finishTurnReactions(sessionID)
	if len(deliveries) != 0 {
		t.Fatalf("expected no tracked reactions, got %+v", deliveries)
	}
}

func TestApplyTurnEndReactionsFallsBackToTurnTrigger(t *testing.T) {
	p := &Platform{
		doneEmoji: reactionDone,
	}
	sessionID := agentkit.SessionID("session-fallback")
	trigger := delivery{channel: "C1", msgTS: "1.0", sessionID: sessionID}
	p.turnTriggers.Store(sessionID, trigger)

	p.applyTurnEndReactions(context.Background(), sessionID, capsession.TurnEndData{Steps: 1})

	if _, still := p.turnTriggers.Load(sessionID); still {
		t.Fatal("turn trigger should be cleared")
	}
}

func TestParseTurnEndDataCancelled(t *testing.T) {
	raw, err := json.Marshal(capsession.TurnEndData{Steps: 2, Cancelled: true, StopReason: "/stop"})
	if err != nil {
		t.Fatal(err)
	}
	data := parseTurnEndData(agentkit.OutboundEvent{Data: raw})
	if !data.Cancelled || data.StopReason != "/stop" || data.Steps != 2 {
		t.Fatalf("unexpected data: %+v", data)
	}
}

func TestResolveSlackReactionEmoji(t *testing.T) {
	if resolveSlackReactionEmoji("", "white_check_mark") != "white_check_mark" {
		t.Fatal("empty should use default")
	}
	if resolveSlackReactionEmoji("none", "white_check_mark") != "" {
		t.Fatal("none should disable")
	}
	if resolveSlackReactionEmoji("thumbsup", "white_check_mark") != "thumbsup" {
		t.Fatal("explicit emoji should pass through")
	}
}
