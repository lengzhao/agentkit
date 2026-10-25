package slack

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestTurnEndReactionsIncludeSteeredMessages(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-steer")

	trigger := delivery{channel: "C1", msgTS: "1.0", sessionID: sessionID}
	steered := delivery{channel: "C1", msgTS: "2.0", sessionID: sessionID}
	p.deliveries.Store(sessionID, trigger)

	p.onTurnStartReactions(sessionID)
	p.appendTurnReactionIfActive(sessionID, steered)

	tracked := p.finishTurnReactions(sessionID)
	if len(tracked) != 2 {
		t.Fatalf("expected trigger + steered, got %+v", tracked)
	}
	if tracked[0].delivery.msgTS != "1.0" || tracked[1].delivery.msgTS != "2.0" {
		t.Fatalf("unexpected order or ts: %+v", tracked)
	}
	for _, entry := range tracked {
		if entry.reaction != reactionReceived {
			t.Fatalf("fresh entries should carry the processing reaction, got %+v", entry)
		}
	}
}

func TestAppendTurnReactionIgnoredWhenTurnInactive(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("session-idle")
	late := delivery{channel: "C1", msgTS: "9.0", sessionID: sessionID}

	p.appendTurnReactionIfActive(sessionID, late)

	tracked := p.finishTurnReactions(sessionID)
	if len(tracked) != 0 {
		t.Fatalf("expected no tracked reactions, got %+v", tracked)
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

func TestApplyTurnSteerSwapsTrackedReaction(t *testing.T) {
	p := &Platform{steerEmoji: reactionSteer}
	sessionID := agentkit.SessionID("slack:C1")
	trigger := delivery{channel: "C1", msgTS: "1.0", sessionID: sessionID}
	steered := delivery{channel: "C1", msgTS: "2.0", sessionID: sessionID}
	p.deliveries.Store(sessionID, trigger)

	p.onTurnStartReactions(sessionID)
	p.appendTurnReactionIfActive(sessionID, steered)

	event := agentkit.OutboundEvent{
		Type:  agentkit.EventTurnSteer,
		Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "slack", DeliveryID: sessionID, ChannelID: "C1", ReplyTo: "2.0"}),
	}
	if err := p.applyTurnSteer(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	// turn/end must retire the steer emoji on the steered message and the
	// untouched processing reaction on the trigger.
	tracked := p.finishTurnReactions(sessionID)
	if len(tracked) != 2 {
		t.Fatalf("tracked = %+v, want trigger + steered", tracked)
	}
	byTS := map[string]string{}
	for _, entry := range tracked {
		byTS[entry.delivery.msgTS] = entry.reaction
	}
	if byTS["2.0"] != reactionSteer {
		t.Fatalf("steered message reaction = %q, want %q", byTS["2.0"], reactionSteer)
	}
	if byTS["1.0"] != reactionReceived {
		t.Fatalf("trigger reaction = %q, want %q", byTS["1.0"], reactionReceived)
	}
}

func TestApplyTurnSteerDisabledWithoutEmoji(t *testing.T) {
	p := &Platform{}
	event := agentkit.OutboundEvent{
		Type:  agentkit.EventTurnSteer,
		Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "slack", DeliveryID: "slack:C1", ChannelID: "C1", ReplyTo: "2.0"}),
	}
	if err := p.applyTurnSteer(context.Background(), event); err != nil {
		t.Fatal(err)
	}
}

func TestApplyTurnSteerWithoutTargetIsNoop(t *testing.T) {
	p := &Platform{steerEmoji: reactionSteer}
	event := agentkit.OutboundEvent{
		Type:  agentkit.EventTurnSteer,
		Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "slack", DeliveryID: "slack:C9"}),
	}
	if err := p.applyTurnSteer(context.Background(), event); err != nil {
		t.Fatal(err)
	}
}

func TestApplyTurnSteerOutsideBatchStillAcknowledges(t *testing.T) {
	p := &Platform{steerEmoji: reactionSteer}
	sessionID := agentkit.SessionID("slack:C2")
	// No onTurnStartReactions: the turn is not tracking anything (already
	// unwinding). The swap has nowhere to land, but must not error.
	event := agentkit.OutboundEvent{
		Type:  agentkit.EventTurnSteer,
		Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "slack", DeliveryID: sessionID, ChannelID: "C2", ReplyTo: "9.0"}),
	}
	if err := p.applyTurnSteer(context.Background(), event); err != nil {
		t.Fatal(err)
	}
}
