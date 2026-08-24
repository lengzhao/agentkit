package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// crashedSession builds the log a process leaves behind when it dies after
// dispatching a tool call but before recording its result.
func crashedSession(t *testing.T) agentkit.Session {
	t.Helper()
	ctx := context.Background()
	sess, err := session.NewMemory(session.MemoryConfig{ID: "test:crashed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendTurnStart(ctx, sess, "coder"); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, "coder", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "refactor it"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendStepStart(ctx, sess, "coder", 0); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendMessage(ctx, sess, "coder", agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role: "assistant",
		ToolCalls: []agentkit.ToolCall{
			{ID: "call-a", Name: "read", Input: []byte(`{"path":"a.go"}`)},
			{ID: "call-b", Name: "read", Input: []byte(`{"path":"b.go"}`)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// Only the first call got an answer before the process died.
	if err := session.AppendToolResult(ctx, sess, "coder", agentkit.ToolResult{
		ID:      "call-a",
		Name:    "read",
		Content: []agentkit.ContentPart{{Type: "text", Text: "package a"}},
	}); err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestScanIncompleteFindsOpenTurnAndOrphans(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := crashedSession(t)
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}

	incomplete := session.ScanIncomplete(events)
	if incomplete == nil {
		t.Fatal("expected the open turn to be detected")
	}
	if incomplete.AgentID != "coder" {
		t.Fatalf("agent id = %q, want coder", incomplete.AgentID)
	}
	if incomplete.StepsStarted != 1 || incomplete.StepsEnded != 0 {
		t.Fatalf("steps started/ended = %d/%d, want 1/0", incomplete.StepsStarted, incomplete.StepsEnded)
	}
	if incomplete.OpenStep != 0 {
		t.Fatalf("open step = %d, want 0", incomplete.OpenStep)
	}
	if len(incomplete.OrphanCalls) != 1 || incomplete.OrphanCalls[0].ID != "call-b" {
		t.Fatalf("orphan calls = %+v, want only call-b", incomplete.OrphanCalls)
	}
}

func TestScanIncompleteIgnoresClosedTurns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess, err := session.NewMemory(session.MemoryConfig{ID: "test:clean"})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AppendTurnStart(ctx, sess, "coder"); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendStepStart(ctx, sess, "coder", 0); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendStepEnd(ctx, sess, "coder", 0); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendTurnEnd(ctx, sess, "coder", 1); err != nil {
		t.Fatal(err)
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if got := session.ScanIncomplete(events); got != nil {
		t.Fatalf("clean log reported incomplete: %+v", got)
	}
}

func TestRepairIncompleteClosesTurnAndAnswersCalls(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := crashedSession(t)
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	incomplete := session.ScanIncomplete(events)

	data, err := session.RepairIncomplete(ctx, sess, incomplete)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if data.OrphanResults != 1 {
		t.Fatalf("orphan results = %d, want 1", data.OrphanResults)
	}
	if data.ClosedStep != 0 {
		t.Fatalf("closed step = %d, want 0", data.ClosedStep)
	}

	events, err = session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	// The turn is closed, so a second pass finds nothing left to repair.
	if got := session.ScanIncomplete(events); got != nil {
		t.Fatalf("repair left the turn open: %+v", got)
	}
	// The repair itself is on the log.
	found := false
	for _, ev := range events {
		if ev.Type == agentkit.EventSessionRecovery {
			found = true
		}
	}
	if !found {
		t.Fatal("no session/recovery event recorded")
	}
}

func TestRepairIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := crashedSession(t)
	for i := 0; i < 2; i++ {
		events, err := session.ReadAllEvents(ctx, sess)
		if err != nil {
			t.Fatal(err)
		}
		incomplete := session.ScanIncomplete(events)
		if i == 1 && incomplete != nil {
			t.Fatalf("second scan still reports work: %+v", incomplete)
		}
		if _, err := session.RepairIncomplete(ctx, sess, incomplete); err != nil {
			t.Fatalf("repair %d: %v", i, err)
		}
	}
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	recoveries := 0
	for _, ev := range events {
		if ev.Type == agentkit.EventSessionRecovery {
			recoveries++
		}
	}
	if recoveries != 1 {
		t.Fatalf("session/recovery events = %d, want 1", recoveries)
	}
}

func TestDeriveAnswersOrphanToolCalls(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := crashedSession(t)

	// Even before any repair, the derived history must be replayable: every
	// assistant tool call needs a reply or the provider rejects the request.
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertToolCallsAnswered(t, messages)

	var interrupted int
	for _, msg := range messages {
		for _, result := range msg.ToolResults {
			if strings.Contains(textOfParts(result.Content), "interrupted") {
				interrupted++
			}
		}
	}
	if interrupted != 1 {
		t.Fatalf("interrupted stand-in results = %d, want 1", interrupted)
	}
}

func TestDeriveKeepsRepeatedToolCallIDsPaired(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess, err := session.NewMemory(session.MemoryConfig{ID: "test:repeatids"})
	if err != nil {
		t.Fatal(err)
	}
	// Scripted and retried runs reuse call IDs across steps. Each call must
	// consume its own later result, not an earlier one.
	for i := 0; i < 2; i++ {
		if err := session.AppendMessage(ctx, sess, "coder", agentkit.EventAssistantMessage, agentkit.ModelMessage{
			Role:      "assistant",
			ToolCalls: []agentkit.ToolCall{{ID: "read-call", Name: "read", Input: []byte(`{"path":"a.go"}`)}},
		}); err != nil {
			t.Fatal(err)
		}
		if err := session.AppendToolResult(ctx, sess, "coder", agentkit.ToolResult{
			ID:      "read-call",
			Name:    "read",
			Content: []agentkit.ContentPart{{Type: "text", Text: "package a"}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertToolCallsAnswered(t, messages)
	for _, msg := range messages {
		for _, result := range msg.ToolResults {
			if strings.Contains(textOfParts(result.Content), "interrupted") {
				t.Fatalf("a properly answered call was treated as orphaned:\n%+v", messages)
			}
		}
	}
}

// assertToolCallsAnswered checks the provider-side invariant: every assistant
// tool call is followed by a tool result carrying the same ID.
func assertToolCallsAnswered(t *testing.T, messages []agentkit.ModelMessage) {
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
				t.Fatalf("tool call %q at index %d has no result:\n%+v", call.ID, i, messages)
			}
		}
	}
}

func textOfParts(parts []agentkit.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.Text)
	}
	return b.String()
}
