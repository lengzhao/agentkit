package common

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestWithDeliveryRouteSetsEnvelope(t *testing.T) {
	t.Parallel()

	delivery := agentkit.SessionID("slack:C001:t:1:u:U1")
	event := WithDeliverySession(agentkit.MessageEvent{
		PlatformID: "slack",
		UserID:     "U1",
	}, "slack", delivery)

	id, ok := session.RouteSessionID(event.Envelope.Route)
	if !ok || id != delivery {
		t.Fatalf("route = %v", event.Envelope.Route)
	}
	if event.Envelope.Route.Platform != "slack" {
		t.Fatalf("route platform = %q", event.Envelope.Route.Platform)
	}
	if event.Envelope.Actor.UserID != "U1" {
		t.Fatalf("actor = %q", event.Envelope.Actor.UserID)
	}
}

func TestWithInboundRoutePreservesReplyTo(t *testing.T) {
	t.Parallel()

	event := WithInboundRoute(agentkit.MessageEvent{
		PlatformID: "slack",
		UserID:     "U1",
	}, session.SessionRouteInput{
		Platform:    "slack",
		DeliveryID:  agentkit.SessionID("slack:C001:t:1:u:U1"),
		ChannelID:   "C001",
		ThreadID:    "1",
		ReplyTo:     "msg-42",
		ScopeUserID: "U1",
	})

	if event.Envelope.Route.Platform != "slack" {
		t.Fatalf("platform = %q", event.Envelope.Route.Platform)
	}
	target, ok := session.DecodeSessionRoute(event.Envelope.Route)
	if !ok {
		t.Fatal("DecodeSessionRoute failed")
	}
	if target.ReplyTo != "msg-42" {
		t.Fatalf("replyTo = %q", target.ReplyTo)
	}
}

func TestInboundMessageSetsDeliveryRoute(t *testing.T) {
	t.Parallel()

	event := InboundMessage("coder", agentkit.SessionID("slack:C001"), "slack", "u1", "hi")
	if event.AgentID != "coder" {
		t.Fatalf("AgentID = %q, want coder", event.AgentID)
	}
	id, ok := session.RouteSessionID(event.Envelope.Route)
	if !ok || id != "slack:C001" {
		t.Fatalf("route id = %v", event.Envelope.Route)
	}
}
