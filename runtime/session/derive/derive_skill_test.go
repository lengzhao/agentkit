package derive_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/skill"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
)

func TestRenderSkillLoadedIncludesResourceBase(t *testing.T) {
	t.Parallel()

	text := derive.RenderSkillLoaded(skill.Content{
		Name: "demo",
		Body: "Do the thing.",
		Path: "/tmp/skills/demo",
	})
	if !strings.Contains(text, `<skill_content name="demo">`) {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "Base directory for this skill: /tmp/skills/demo") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "Read supporting files with read") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "Do the thing.") {
		t.Fatalf("text = %q", text)
	}
}

func TestDeriveMessagesSkillLoadAfterToolResult(t *testing.T) {
	t.Parallel()

	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{AgentID: agentkit.AgentID("assistant")})
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "mem-skill-order"})
	if err != nil {
		t.Fatal(err)
	}

	if err := sessevents.Default.AppendMessage(ctx, sess, "assistant", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "load skill"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendMessage(ctx, sess, "assistant", agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role: "assistant",
		ToolCalls: []agentkit.ToolCall{{
			ID: "call-skill", Name: "skill", Input: []byte(`{"name":"feedback-ticket-intake"}`),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := derive.AppendSkillLoad(ctx, sess, "assistant", skill.Content{
		Name:        "feedback-ticket-intake",
		Description: "Triage feedback",
		Body:        "Follow these steps.",
		Path:        "/skills/feedback-ticket-intake",
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendToolResult(ctx, sess, "assistant", agentkit.ToolResult{
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
	if msgs[3].Role != "user" || !strings.Contains(msgs[3].Content[0].Text, `<skill_content name="feedback-ticket-intake">`) {
		t.Fatalf("message 3 = %+v, want skill load user message", msgs[3])
	}
}
