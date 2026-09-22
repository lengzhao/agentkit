package agenttest

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

// ContentText joins text parts from a model message.
func ContentText(msg agentkit.ModelMessage) string {
	var b strings.Builder
	for _, part := range msg.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// CountEvents counts session events of a given type.
func CountEvents(events []agentkit.SessionEvent, typ agentkit.EventType) int {
	n := 0
	for _, ev := range events {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

// AssertDeriveMessagesReplayPairing checks derive replay invariants for LLM providers
// (answered tool calls, no leading orphan tool messages).
func AssertDeriveMessagesReplayPairing(t *testing.T, sess agentkit.Session, ctx context.Context) {
	t.Helper()
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	AssertModelMessagesReplayPairing(t, messages)
}

// AssertModelMessagesReplayPairing applies replay invariants to an already-derived slice.
func AssertModelMessagesReplayPairing(t *testing.T, messages []agentkit.ModelMessage) {
	t.Helper()
	if len(messages) > 0 && len(messages[0].ToolResults) > 0 {
		t.Fatalf("derived history must not start with tool message: %#v", messages[0])
	}
	AssertDeriveMessagesToolCallsAnsweredMessages(t, messages)
}

// AssertDeriveMessagesToolCallsAnswered checks provider replay invariants.
func AssertDeriveMessagesToolCallsAnswered(t *testing.T, sess agentkit.Session, ctx context.Context) {
	t.Helper()
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	AssertDeriveMessagesToolCallsAnsweredMessages(t, messages)
}

func AssertDeriveMessagesToolCallsAnsweredMessages(t *testing.T, messages []agentkit.ModelMessage) {
	t.Helper()
	for i, msg := range messages {
		for _, call := range msg.ToolCalls {
			answered := false
			for _, later := range messages[i+1:] {
				for _, result := range later.ToolResults {
					if result.ID == call.ID {
						answered = true
					}
				}
			}
			if !answered {
				t.Fatalf("tool call %q has no result in derived history", call.ID)
			}
		}
	}
}

// ToolResult decodes a tool/result event payload.
func ToolResult(t *testing.T, ev agentkit.SessionEvent) agentkit.ToolResult {
	t.Helper()
	var result agentkit.ToolResult
	if err := json.Unmarshal(ev.Data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// SubagentStart decodes a subagent/start event payload.
func SubagentStart(t *testing.T, ev agentkit.SessionEvent) sessevents.SubagentStartData {
	t.Helper()
	var data sessevents.SubagentStartData
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

// AssertNoToolResultWithContent fails if any tool result for callID contains substr.
func AssertNoToolResultWithContent(t *testing.T, events []agentkit.SessionEvent, callID agentkit.ToolCallID, substr string) {
	t.Helper()
	for _, ev := range events {
		if ev.Type != agentkit.EventToolResult {
			continue
		}
		result := ToolResult(t, ev)
		if result.ID == callID && strings.Contains(result.Content, substr) {
			t.Fatalf("unexpected %q in tool result for %s: %+v", substr, callID, result)
		}
	}
}

// AssertToolResultContains finds a tool result by call id and checks content.
func AssertToolResultContains(t *testing.T, events []agentkit.SessionEvent, callID agentkit.ToolCallID, substr string) {
	t.Helper()
	for _, ev := range events {
		if ev.Type != agentkit.EventToolResult {
			continue
		}
		result := ToolResult(t, ev)
		if result.ID != callID {
			continue
		}
		if !strings.Contains(result.Content, substr) {
			t.Fatalf("tool result for %s = %q, want substring %q", callID, result.Content, substr)
		}
		return
	}
	t.Fatalf("no tool result for %s", callID)
}

// AssertEventAtLeast fails when an event type appears fewer than min times.
func AssertEventAtLeast(t *testing.T, events []agentkit.SessionEvent, typ agentkit.EventType, min int) {
	t.Helper()
	if got := CountEvents(events, typ); got < min {
		t.Fatalf("%s = %d, want at least %d", typ, got, min)
	}
}
