package slack

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestDeliveryForReactionFromRoute(t *testing.T) {
	p := &Platform{}
	sessionID := agentkit.SessionID("slack:C1")
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{
		Platform: "slack", DeliveryID: sessionID, ChannelID: "C1", ThreadID: "1.0", ReplyTo: "2.0",
	})
	event := agentkit.OutboundEvent{Route: route}
	d, ok := p.deliveryForReaction(sessionID, event)
	if !ok {
		t.Fatal("expected a reaction target")
	}
	if d.channel != "C1" || d.msgTS != "2.0" {
		t.Fatalf("delivery = %+v", d)
	}
	// Steered message outside a thread: threadTS is empty, that is fine.
	route2 := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "slack", DeliveryID: sessionID, ChannelID: "C1", ReplyTo: "3.0"})
	if d2, ok := p.deliveryForReaction(sessionID, agentkit.OutboundEvent{Route: route2}); !ok || d2.msgTS != "3.0" || d2.threadTS != "" {
		t.Fatalf("threadless delivery = %+v ok=%v", d2, ok)
	}
}
