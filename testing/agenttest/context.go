package agenttest

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
)

// TurnContext returns a context seeded for agent.RunTurn.
func TurnContext(sessionID agentkit.SessionID, agentID agentkit.AgentID) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeySessionID, sessionID)
	if agentID != "" {
		ctx = context.WithValue(ctx, agentkit.KeyAgentID, agentID)
	}
	return ctx
}

// LoopTurnContext mimics loop/default seeding with a resolved SessionID.
func LoopTurnContext(sessionID agentkit.SessionID, agentID agentkit.AgentID) context.Context {
	return TurnContext(sessionID, agentID)
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
