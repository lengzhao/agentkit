package learning

import (
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestDecideMemoryNudgeSkipsWhenMemoryToolUsed(t *testing.T) {
	msgs := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "call me 小飞"}}},
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{Name: "memory", Input: json.RawMessage(`{}`)}}},
	}
	dec, st := DecideMemoryNudge(10, msgs, NudgeSessionState{TurnsSinceMemory: 9}, false)
	if dec.RunReview || st.TurnsSinceMemory != 0 {
		t.Fatalf("dec=%+v st=%+v", dec, st)
	}
}

func TestDecideMemoryNudgeFiresOnInterval(t *testing.T) {
	msgs := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}},
	}
	dec, st := DecideMemoryNudge(3, msgs, NudgeSessionState{TurnsSinceMemory: 2}, false)
	if !dec.RunReview || st.TurnsSinceMemory != 0 {
		t.Fatalf("dec=%+v st=%+v", dec, st)
	}
}

func TestDecideMemoryNudgeDisabledWhenIntervalZero(t *testing.T) {
	msgs := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}},
	}
	dec, _ := DecideMemoryNudge(0, msgs, NudgeSessionState{TurnsSinceMemory: 99}, false)
	if dec.RunReview {
		t.Fatal("expected no review")
	}
}
