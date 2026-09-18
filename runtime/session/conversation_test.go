package session_test

import (
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestConversationFromEventUsesEnvelope(t *testing.T) {
	t.Parallel()

	event := agentkit.MessageEvent{
		Envelope: agentkit.TurnEnvelope{
			Conversation: "slack:C001:new",
		},
	}
	if got := rctx.ConversationFromEvent(event); got != "slack:C001:new" {
		t.Fatalf("got %q", got)
	}
}

func TestConversationFromEventRequiresEnvelope(t *testing.T) {
	t.Parallel()

	event := agentkit.MessageEvent{}
	if got := rctx.ConversationFromEvent(event); got != "" {
		t.Fatalf("got %q, want empty without envelope conversation", got)
	}
}

func TestConversationFromLoopRequestUsesEventEnvelope(t *testing.T) {
	t.Parallel()

	req := agentkit.LoopRequest{
		Event: agentkit.MessageEvent{
			Envelope: agentkit.TurnEnvelope{Conversation: "slack:C001"},
		},
	}
	if got := rctx.ConversationFromLoopRequest(req); got != "slack:C001" {
		t.Fatalf("got %q", got)
	}
}
