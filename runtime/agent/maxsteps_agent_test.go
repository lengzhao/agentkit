package agent_test

import (
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/llm"
)

func readStep() llm.ScriptedStep {
	return llm.ScriptedStep{ToolCalls: []agentkit.ToolCall{llm.MustToolCall("read", `{"path":"README.md"}`)}}
}

func TestMaxStepsDefaultCapsTurn(t *testing.T) {
	t.Parallel()

	steps := []llm.ScriptedStep{readStep(), readStep(), readStep(), readStep(), readStep()}
	f := newTurnFixture(t, nil, agent.Config{ID: "test", MaxSteps: intPtr(2)}, steps)
	if err := f.run(t); !agent.IsStepLimitError(err) {
		t.Fatalf("run turn: %v, want step-limit", err)
	}

	events := f.sessionEvents(t)
	if got := countEvents(events, agentkit.EventStepStart); got != 2 {
		t.Fatalf("step/start events = %d, want 2", got)
	}

	var end capsession.TurnEndData
	for _, ev := range events {
		if ev.Type == agentkit.EventTurnEnd {
			if err := json.Unmarshal(ev.Data, &end); err != nil {
				t.Fatalf("decode turn/end: %v", err)
			}
		}
	}
	if end.Steps != 2 {
		t.Fatalf("turn/end steps = %d, want 2", end.Steps)
	}
	if end.StopReason != string(agentkit.StopStepLimit) {
		t.Fatalf("turn/end stop reason = %q, want %q", end.StopReason, agentkit.StopStepLimit)
	}
	if end.StepLimit != 2 {
		t.Fatalf("turn/end stepLimit = %d, want 2", end.StepLimit)
	}
	notice := agent.StepLimitUserMessage(2)
	for _, ev := range events {
		if ev.Type != agentkit.EventAssistantMessage {
			continue
		}
		var msg agentkit.ModelMessage
		if err := json.Unmarshal(ev.Data, &msg); err != nil {
			continue
		}
		for _, part := range msg.Content {
			if part.Type == "text" && part.Text == notice {
				t.Fatalf("step limit notice should not be persisted in session (loop outbound only)")
			}
		}
	}
}

func TestMaxStepsZeroDisablesCap(t *testing.T) {
	t.Parallel()

	steps := []llm.ScriptedStep{readStep(), readStep(), {Text: "done"}}
	f := newTurnFixture(t, nil, agent.Config{ID: "test", MaxSteps: intPtr(0)}, steps)
	if err := f.run(t); err != nil {
		t.Fatalf("run turn: %v", err)
	}
	if got := countEvents(f.sessionEvents(t), agentkit.EventStepStart); got != 3 {
		t.Fatalf("step/start events = %d, want 3", got)
	}
}

func intPtr(n int) *int { return &n }
