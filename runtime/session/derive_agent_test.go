package session_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestDeriveMessagesFiltersByAgent(t *testing.T) {
	t.Parallel()

	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{AgentID: agentkit.AgentID("meetingbot")})
	mem, err := session.NewMemory(session.MemoryConfig{ID: "mem-test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct {
		agent agentkit.AgentID
		text  string
	}{
		{"assistant", "old assistant turn"},
		{"meetingbot", "my question"},
	} {
		raw, _ := json.Marshal(agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: spec.text}},
		})
		if _, err := mem.Append(ctx, agentkit.SessionEvent{
			AgentID: spec.agent,
			Type:    agentkit.EventUserMessage,
			Data:    raw,
		}); err != nil {
			t.Fatal(err)
		}
	}

	msgs, err := mem.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1 agent-scoped user message", len(msgs))
	}
	if msgs[0].Content[0].Text != "my question" {
		t.Fatalf("got %q", msgs[0].Content[0].Text)
	}
}
