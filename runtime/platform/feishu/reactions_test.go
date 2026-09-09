package feishu

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestTurnEndReactionsIncludeSteeredMessages(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-steer")

	trigger := replyContext{messageID: "msg-1", processingReactionID: "rx-1"}
	steered := replyContext{messageID: "msg-2", processingReactionID: "rx-2"}
	p.deliveries.Store(sessionID, trigger)

	p.onTurnStartReactions(sessionID)
	p.appendTurnReactionIfActive(sessionID, steered)

	rcs := p.finishTurnReactions(sessionID)
	if len(rcs) != 2 {
		t.Fatalf("expected trigger + steered, got %+v", rcs)
	}
	if rcs[0].messageID != "msg-1" || rcs[1].messageID != "msg-2" {
		t.Fatalf("unexpected order or ids: %+v", rcs)
	}
}

func TestAppendTurnReactionIgnoredWhenTurnInactive(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-idle")
	late := replyContext{messageID: "msg-late", processingReactionID: "rx-late"}

	p.appendTurnReactionIfActive(sessionID, late)

	rcs := p.finishTurnReactions(sessionID)
	if len(rcs) != 0 {
		t.Fatalf("expected no tracked reactions, got %+v", rcs)
	}
}

func TestApplyTurnEndReactionsFallsBackToTurnTrigger(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-fallback")
	trigger := replyContext{messageID: "trigger-msg", processingReactionID: "rx-1"}
	p.turnTriggers.Store(sessionID, trigger)

	p.applyTurnEndReactions(sessionID, session.TurnEndData{Steps: 1})

	if _, still := p.turnTriggers.Load(sessionID); still {
		t.Fatal("turn trigger should be cleared")
	}
}

func TestBotReplyMessageIDPrefersUnifiedCard(t *testing.T) {
	st := &streamState{
		cardHandle: &feishuPreviewHandle{messageID: "card-1"},
		bodyHandle: &feishuPreviewHandle{messageID: "body-1"},
	}
	if id := botReplyMessageID(st); id != "card-1" {
		t.Fatalf("messageID = %q, want card-1", id)
	}
}

func TestBotReplyMessageIDPrefersBodyCard(t *testing.T) {
	st := &streamState{
		bodyHandle: &feishuPreviewHandle{messageID: "body-1"},
		progressHandle: &feishuPreviewHandle{messageID: "progress-1"},
	}
	if id := botReplyMessageID(st); id != "body-1" {
		t.Fatalf("messageID = %q, want body-1", id)
	}
}
