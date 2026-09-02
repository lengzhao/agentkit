package feishu

import (
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestApplyRichStreamEventToolAndThinking(t *testing.T) {
	p := &Platform{
		progressStyle:    "card",
		showThinking:     true,
		showToolProgress: true,
	}
	st := &streamState{toolStepIdx: make(map[int]int)}

	if !p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:  agentkit.AssistantEventThinkingDelta,
		Delta: "plan",
	}) {
		t.Fatal("expected thinking delta to update state")
	}
	if st.thinking != "plan" {
		t.Fatalf("thinking = %q", st.thinking)
	}

	if !p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:         agentkit.AssistantEventToolCallStart,
		ContentIndex: 1,
		ToolName:     "Read",
	}) {
		t.Fatal("expected tool start to update state")
	}
	if len(st.steps) != 1 || st.steps[0].Name != "Read" {
		t.Fatalf("steps = %#v", st.steps)
	}

	if !p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:         agentkit.AssistantEventToolCallEnd,
		ContentIndex: 1,
		ToolCall:     &agentkit.ToolCall{Name: "Read", Input: []byte(`{"path":"README.md"}`)},
	}) {
		t.Fatal("expected tool end to update state")
	}
	if !st.steps[0].Done {
		t.Fatalf("step not done: %#v", st.steps[0])
	}

	if !p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:  agentkit.AssistantEventTextDelta,
		Delta: "hello",
	}) {
		t.Fatal("expected text delta to update state")
	}
	if st.text != "hello" {
		t.Fatalf("text = %q", st.text)
	}
}

func TestRenderRichStreamContentUsesSingleCard(t *testing.T) {
	p := &Platform{
		progressStyle:    "card",
		showThinking:     true,
		showToolProgress: true,
	}
	st := &streamState{
		status: cardStatusWorking,
		text:   "final answer",
		thinking: "hmm",
		steps: []toolStep{{
			Kind: toolStepKindTool,
			Name: "Grep",
			Summary: "pattern",
			Done: true,
		}},
	}
	card := p.renderRichStreamContent(st, true)
	if !strings.Contains(card, `"schema"`) {
		t.Fatalf("expected card json, got %q", card)
	}
	if !strings.Contains(card, "final answer") {
		t.Fatalf("expected answer in card: %q", card)
	}
	if !strings.Contains(card, "collapsible_panel") {
		t.Fatalf("expected progress panel in card: %q", card)
	}
}

func TestLegacyStreamUpdateIgnoresThinkingByDefault(t *testing.T) {
	p := &Platform{progressStyle: "legacy", showThinking: false}
	st := &streamState{}
	if p.applyRichStreamEvent(st, agentkit.AssistantMessageEvent{
		Type:  agentkit.AssistantEventThinkingDelta,
		Delta: "secret",
	}) {
		t.Fatal("legacy mode should ignore thinking when disabled")
	}
}

func TestRenderCompactProgressCard(t *testing.T) {
	p := &Platform{
		progressStyle:    "compact",
		showThinking:     true,
		showToolProgress: true,
	}
	st := &streamState{
		thinking: "plan",
		steps: []toolStep{{
			Kind:    toolStepKindTool,
			Name:    "Bash",
			Summary: "ls",
			Done:    true,
			Status:  "completed",
		}},
		text: "done",
	}
	content := p.renderRichStreamContent(st, false)
	if !strings.HasPrefix(content, "__cc_connect_progress_card_v1__:") {
		t.Fatalf("expected compact payload prefix, got %q", content)
	}
}
