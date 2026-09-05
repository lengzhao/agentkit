package agenttest

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// TurnContext returns a context seeded for agent.RunTurn.
func TurnContext(sessionID agentkit.SessionID, agentID agentkit.AgentID) context.Context {
	conv := string(sessionID)
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: conv,
		AgentID:      agentID,
	})
	return ctx
}

// LoopRequest builds a LoopRequest with conversation on the event envelope.
func LoopRequest(conversation agentkit.SessionID, event agentkit.MessageEvent) agentkit.LoopRequest {
	event.Envelope.Conversation = string(conversation)
	return agentkit.LoopRequest{
		Event: event,
	}
}

// RunTurn is a thin helper around agent.RunTurn with a user text message.
func RunTurn(t *testing.T, ctx context.Context, ag agentkit.Agent, text string) {
	t.Helper()
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}
}
