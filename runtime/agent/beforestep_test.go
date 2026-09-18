package agent_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/llm"
)

// recordBeforeStep captures every BeforeStep payload and drives one
// continuation, so the test can assert Step/Segment across segments.
type recordBeforeStep struct {
	continueTexts []string
	seen          []agentkit.BeforeStep
}

func (h *recordBeforeStep) BeforeStep(_ context.Context, in *agentkit.BeforeStep) error {
	h.seen = append(h.seen, *in)
	return nil
}

func (h *recordBeforeStep) BeforeTool(context.Context, *agentkit.ToolCall) error { return nil }
func (h *recordBeforeStep) AfterTool(context.Context, *agentkit.ToolResult) error { return nil }

func (h *recordBeforeStep) TurnComplete(context.Context, *agentkit.TurnComplete) error { return nil }

func (h *recordBeforeStep) TurnStopping(_ context.Context, in *agentkit.TurnStopping) error {
	if len(h.continueTexts) > 0 {
		text := h.continueTexts[0]
		h.continueTexts = h.continueTexts[1:]
		in.Continue = append(in.Continue, agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		})
	}
	return nil
}

func TestBeforeStepCarriesStepAndSegment(t *testing.T) {
	t.Parallel()

	hooks := &recordBeforeStep{continueTexts: []string{"keep going"}}
	steps := []llm.ScriptedStep{
		{ToolCalls: []agentkit.ToolCall{llm.MustToolCall("read", `{"path":"README.md"}`)}},
		{Text: "reply"},
		{Text: "reply again"},
	}
	f := newTurnFixture(t, hooks, agent.Config{ID: "test"}, steps)
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	// Segment 0 runs a tool step and a text step; the continuation adds one
	// more step in segment 1. Step indexes are turn-wide and 0-based.
	want := []struct{ step, segment int }{{0, 0}, {1, 0}, {2, 1}}
	if len(hooks.seen) != len(want) {
		t.Fatalf("before-step calls = %d, want %d", len(hooks.seen), len(want))
	}
	for i, w := range want {
		got := hooks.seen[i]
		if got.Step != w.step || got.Segment != w.segment {
			t.Errorf("call %d: Step=%d Segment=%d, want Step=%d Segment=%d",
				i, got.Step, got.Segment, w.step, w.segment)
		}
	}
}
