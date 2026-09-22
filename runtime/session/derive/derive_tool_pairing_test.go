package derive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
)

func TestRepairToolPairingDropsLeadingOrphan(t *testing.T) {
	t.Parallel()

	out := repairToolPairing([]agentkit.ModelMessage{
		{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "c1", Name: "read", Content: "orphan"}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}},
	})
	if len(out) != 1 || out[0].Role != "user" {
		t.Fatalf("got %#v, want single user message", out)
	}
}

func TestRepairToolPairingKeepsMatchedRound(t *testing.T) {
	t.Parallel()

	in := []agentkit.ModelMessage{
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "c1", Name: "read"}}},
		{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "c1", Name: "read", Content: "ok"}}},
	}
	out := repairToolPairing(in)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
}

func TestRepairToolPairingDropsUnmatchedID(t *testing.T) {
	t.Parallel()

	out := repairToolPairing([]agentkit.ModelMessage{
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "c1", Name: "read"}}},
		{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "other", Name: "read", Content: "nope"}}},
	})
	if len(out) != 2 {
		t.Fatalf("len = %d, want assistant + synthetic tool", len(out))
	}
	if out[1].Role != "tool" || out[1].ToolResults[0].ID != "c1" {
		t.Fatalf("synthetic result missing: %#v", out[1])
	}
}

func TestRepairToolPairingSkipsErroredAssistantAndOrphanTool(t *testing.T) {
	t.Parallel()

	out := repairToolPairing([]agentkit.ModelMessage{
		{Role: "assistant", StopReason: "error", ToolCalls: []agentkit.ToolCall{{ID: "c1", Name: "read"}}},
		{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "c1", Name: "read", Content: "late"}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "retry"}}},
	})
	if len(out) != 1 || out[0].Role != "user" {
		t.Fatalf("got %#v, want only user after skipped error turn", out)
	}
}

func TestRepairToolPairingSyntheticBeforeUser(t *testing.T) {
	t.Parallel()

	out := repairToolPairing([]agentkit.ModelMessage{
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "c1", Name: "read"}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "next"}}},
	})
	if len(out) != 3 {
		t.Fatalf("len = %d, want assistant + synthetic tool + user", len(out))
	}
	if out[1].Role != "tool" || out[1].ToolResults[0].ID != "c1" {
		t.Fatalf("synthetic tool result missing: %#v", out[1])
	}
}

func TestDeriveMessagesStripsOrphanToolBeforeUser(t *testing.T) {
	t.Parallel()

	rawResult, err := json.Marshal(agentkit.ToolResult{ID: "c1", Name: "read", Content: "leftover"})
	if err != nil {
		t.Fatal(err)
	}
	rawUser, err := json.Marshal(agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "continue"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := []agentkit.SessionEvent{
		{Seq: 1, Type: agentkit.EventToolResult, Data: rawResult},
		{Seq: 2, Type: agentkit.EventUserMessage, Data: rawUser},
	}
	msgs := DeriveMessages(context.Background(), events, 0)
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("derived %#v, want lone user message", msgs)
	}
}

func TestDeriveMessagesOrphanCallSyntheticBeforeUser(t *testing.T) {
	t.Parallel()

	rawAssistant, _ := json.Marshal(agentkit.ModelMessage{
		Role:      "assistant",
		ToolCalls: []agentkit.ToolCall{{ID: "c1", Name: "read"}},
	})
	rawUser, _ := json.Marshal(agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "next"}},
	})
	events := []agentkit.SessionEvent{
		{Seq: 1, Type: agentkit.EventAssistantMessage, Data: rawAssistant},
		{Seq: 2, Type: agentkit.EventUserMessage, Data: rawUser},
	}
	msgs := DeriveMessages(context.Background(), events, 0)
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want assistant + synthetic tool + user", len(msgs))
	}
	if msgs[1].Role != "tool" || len(msgs[1].ToolResults) != 1 || msgs[1].ToolResults[0].ID != "c1" {
		t.Fatalf("synthetic tool result missing: %#v", msgs[1])
	}
}

// Pi transformMessages: trailing assistant tool_calls with no toolResult.
func TestRepairToolPairingTrailingOrphanToolCalls(t *testing.T) {
	t.Parallel()

	out := repairToolPairing([]agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "read the file"}}},
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "call_123", Name: "read"}}},
	})
	if len(out) != 3 {
		t.Fatalf("len = %d, want user + assistant + synthetic tool", len(out))
	}
	last := out[len(out)-1]
	if last.Role != "tool" || len(last.ToolResults) != 1 || last.ToolResults[0].ID != "call_123" {
		t.Fatalf("trailing synthetic missing: %#v", last)
	}
}

// Pi transformMessages: only missing calls get synthetic after partial results.
func TestRepairToolPairingPartialToolResults(t *testing.T) {
	t.Parallel()

	out := repairToolPairing([]agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "run"}}},
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{
			{ID: "call_1", Name: "read"},
			{ID: "call_2", Name: "bash"},
		}},
		{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "call_1", Name: "read", Content: "done"}}},
	})
	if len(out) != 4 {
		t.Fatalf("len = %d, want user + assistant + real tool + synthetic tool", len(out))
	}
	synthetic := out[3]
	if synthetic.Role != "tool" || len(synthetic.ToolResults) != 1 || synthetic.ToolResults[0].ID != "call_2" {
		t.Fatalf("synthetic for call_2 missing: %#v", synthetic)
	}
}

// Pi: new assistant closes the previous tool round before starting the next.
func TestRepairToolPairingSyntheticBeforeNewAssistant(t *testing.T) {
	t.Parallel()

	out := repairToolPairing([]agentkit.ModelMessage{
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "c1", Name: "read"}}},
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "c2", Name: "bash"}}},
	})
	if len(out) != 4 {
		t.Fatalf("len = %d, want asst1 + synthetic + asst2 + trailing synthetic", len(out))
	}
	if out[1].Role != "tool" || out[1].ToolResults[0].ID != "c1" {
		t.Fatalf("round 1 synthetic missing: %#v", out[1])
	}
	if out[2].Role != "assistant" || out[2].ToolCalls[0].ID != "c2" {
		t.Fatalf("round 2 assistant missing: %#v", out[2])
	}
	if out[3].Role != "tool" || out[3].ToolResults[0].ID != "c2" {
		t.Fatalf("round 2 trailing synthetic missing: %#v", out[3])
	}
}

func TestRepairToolPairingSkipsAbortedAssistant(t *testing.T) {
	t.Parallel()

	out := repairToolPairing([]agentkit.ModelMessage{
		{Role: "assistant", StopReason: "aborted", ToolCalls: []agentkit.ToolCall{{ID: "c1", Name: "read"}}},
		{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "c1", Name: "read", Content: "x"}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "again"}}},
	})
	if len(out) != 1 || out[0].Role != "user" {
		t.Fatalf("got %#v, want lone user", out)
	}
}

func TestRepairToolPairingSyntheticUsesInterruptedStandIn(t *testing.T) {
	t.Parallel()

	out := repairToolPairing([]agentkit.ModelMessage{
		{Role: "assistant", ToolCalls: []agentkit.ToolCall{{ID: "c1", Name: "read"}}},
	})
	if len(out) != 2 {
		t.Fatal(out)
	}
	tr := out[1].ToolResults[0]
	if tr.Name != "read" || !strings.Contains(tr.Content, "interrupted") {
		t.Fatalf("unexpected stand-in: %#v", tr)
	}
	if tr.Audit["decision"] != "interrupted" {
		t.Fatalf("audit = %#v", tr.Audit)
	}
}

func TestDeriveMessagesCompactionRetainedTailDropsLeadingOrphanTool(t *testing.T) {
	t.Parallel()

	compRaw, err := json.Marshal(capscompaction.EventData{
		Kind:         capscompaction.KindSummary,
		FirstKeptSeq: 10,
		Summary: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "[Conversation summary]\ncontext"}},
		},
		RetainedTail: []agentkit.ModelMessage{
			{Role: "tool", ToolResults: []agentkit.ToolResult{{ID: "orphan", Name: "read", Content: "broken tail"}}},
			{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "recent question"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := []agentkit.SessionEvent{{
		Seq:  5,
		Type: agentkit.EventCompaction,
		Data: compRaw,
	}}
	msgs := DeriveMessages(context.Background(), events, 0)
	if len(msgs) != 2 {
		t.Fatalf("len = %d, want summary + recent user", len(msgs))
	}
	if msgs[0].Role != "user" || !strings.Contains(msgs[0].Content[0].Text, "summary") {
		t.Fatalf("summary missing: %#v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content[0].Text != "recent question" {
		t.Fatalf("recent user missing: %#v", msgs[1])
	}
}

func TestDeriveMessagesNeverStartsWithOrphanTool(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		events []agentkit.SessionEvent
	}{
		{
			name: "orphan tool event before user",
			events: func() []agentkit.SessionEvent {
				rawResult, _ := json.Marshal(agentkit.ToolResult{ID: "c1", Name: "read", Content: "x"})
				rawUser, _ := json.Marshal(agentkit.ModelMessage{
					Role:    "user",
					Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}},
				})
				return []agentkit.SessionEvent{
					{Seq: 1, Type: agentkit.EventToolResult, Data: rawResult},
					{Seq: 2, Type: agentkit.EventUserMessage, Data: rawUser},
				}
			}(),
		},
		{
			name: "errored assistant then tool",
			events: func() []agentkit.SessionEvent {
				rawAsst, _ := json.Marshal(agentkit.ModelMessage{
					Role:       "assistant",
					StopReason: "error",
					ToolCalls:  []agentkit.ToolCall{{ID: "c1", Name: "read"}},
				})
				rawResult, _ := json.Marshal(agentkit.ToolResult{ID: "c1", Name: "read", Content: "x"})
				rawUser, _ := json.Marshal(agentkit.ModelMessage{
					Role:    "user",
					Content: []agentkit.ContentPart{{Type: "text", Text: "retry"}},
				})
				return []agentkit.SessionEvent{
					{Seq: 1, Type: agentkit.EventAssistantMessage, Data: rawAsst},
					{Seq: 2, Type: agentkit.EventToolResult, Data: rawResult},
					{Seq: 3, Type: agentkit.EventUserMessage, Data: rawUser},
				}
			}(),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msgs := DeriveMessages(context.Background(), tc.events, 0)
			if len(msgs) == 0 {
				return
			}
			if len(msgs[0].ToolResults) > 0 {
				t.Fatalf("derived history must not start with tool: %#v", msgs)
			}
		})
	}
}
