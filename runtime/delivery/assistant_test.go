package delivery_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session"
)

type recordingSender struct {
	sent []agentkit.OutboundEvent
}

func (r *recordingSender) Send(_ context.Context, ev agentkit.OutboundEvent) error {
	r.sent = append(r.sent, ev)
	return nil
}

func TestSendAssistantTextEmptyNoOp(t *testing.T) {
	t.Parallel()
	sender := &recordingSender{}
	if err := delivery.SendAssistantText(context.Background(), sender, "  ", delivery.AssistantMessageOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("expected no send, got %d", len(sender.sent))
	}
}

func TestSendProactiveInboxTextUsesEmit(t *testing.T) {
	t.Parallel()

	platform := &recordingSender{}
	var emitted []agentkit.OutboundEvent
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Route:        session.SessionRoute("slack", "slack:C001"),
		Conversation: "slack:C001",
		Workspace:    "slack:C001",
	})
	ctx = session.ContextWithDeliveryRoute(ctx, "slack", agentkit.SessionID("slack:C001:t:1:u:U1"))
	ctx = rctx.WithAgentID(ctx, agentkit.AgentID("coder"))
	ctx = rctx.ContextWithOutboundEmit(ctx, func(_ context.Context, ev agentkit.OutboundEvent) error {
		emitted = append(emitted, ev)
		return nil
	})

	if err := delivery.SendProactiveInboxText(ctx, platform, "hello"); err != nil {
		t.Fatal(err)
	}
	if len(platform.sent) != 0 {
		t.Fatalf("sender should not be used when emit wired, sent=%d", len(platform.sent))
	}
	if len(emitted) != 1 {
		t.Fatalf("emit count=%d", len(emitted))
	}
	if emitted[0].Type != agentkit.EventAssistantMessage {
		t.Fatalf("type=%s", emitted[0].Type)
	}
}

func TestSendAssistantMessageUsesSenderWhenNoEmit(t *testing.T) {
	t.Parallel()

	sender := &recordingSender{}
	ctx := session.ContextWithDeliveryRoute(context.Background(), "slack", agentkit.SessionID("slack:C002"))
	ctx = rctx.WithAgentID(ctx, agentkit.AgentID("a1"))

	err := delivery.SendAssistantText(ctx, sender, "hi", delivery.AssistantMessageOptions{
		Route: capsdelivery.RouteInput{SessionID: "slack:C002"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent=%d", len(sender.sent))
	}
}
