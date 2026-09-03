package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

func TestDeriveMessagesSkillLoadAfterToolResult(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), agentkit.KeyAgentID, agentkit.AgentID("assistant"))
	sess, err := session.NewMemory(session.MemoryConfig{ID: "mem-skill-order"})
	if err != nil {
		t.Fatal(err)
	}

	if err := session.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "load skill"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, "assistant", agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role: "assistant",
		ToolCalls: []agentkit.ToolCall{{
			ID: "call-skill", Name: "skill", Input: []byte(`{"name":"feedback-ticket-intake"}`),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendSkillLoad(ctx, sess, "assistant", "feedback-ticket-intake", "Triage feedback", "Follow these steps."); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendToolResult(ctx, sess, "assistant", agentkit.ToolResult{
		ID:      "call-skill",
		Name:    "skill",
		Content: `{"name":"feedback-ticket-intake"}`,
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("len = %d, want 4 messages", len(msgs))
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("message 1 = %+v, want assistant with tool_calls", msgs[1])
	}
	if msgs[2].Role != "tool" {
		t.Fatalf("message 2 role = %q, want tool before skill load", msgs[2].Role)
	}
	if msgs[3].Role != "user" || !strings.Contains(msgs[3].Content[0].Text, `<skill name="feedback-ticket-intake">`) {
		t.Fatalf("message 3 = %+v, want skill load user message", msgs[3])
	}
}
