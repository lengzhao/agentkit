package sessevents_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
)

func newRunStateSession(t *testing.T) agentkit.Session {
	t.Helper()
	sess, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "test:runstate"})
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestLatestTodosUsesMostRecentSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := newRunStateSession(t)
	if err := sessevents.Default.AppendTodoUpdate(ctx, sess, "a", []capsession.Todo{
		{ID: "1", Title: "first", Status: capsession.TodoPending},
		{ID: "2", Title: "second", Status: capsession.TodoPending},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendTodoUpdate(ctx, sess, "a", []capsession.Todo{
		{ID: "1", Title: "first", Status: capsession.TodoDone},
		{ID: "2", Title: "second", Status: capsession.TodoInProgress},
	}); err != nil {
		t.Fatal(err)
	}

	events, err := derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	todos := capsession.LatestTodos(events)
	if len(todos) != 2 {
		t.Fatalf("todos = %d, want 2", len(todos))
	}
	if todos[0].Status != capsession.TodoDone {
		t.Fatalf("todo 1 status = %q, want done", todos[0].Status)
	}
	pending := capsession.PendingTodos(todos)
	if len(pending) != 1 || pending[0].ID != "2" {
		t.Fatalf("pending = %+v, want only id 2", pending)
	}
}

func TestFinishAfterIgnoresEarlierRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := newRunStateSession(t)
	// A previous run finished, then a new user message started a new run.
	if err := sessevents.Default.AppendRunFinish(ctx, sess, "a", capsession.RunFinishData{
		Status:  capsession.FinishCompleted,
		Summary: "old run",
	}); err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "new task"}},
	}); err != nil {
		t.Fatal(err)
	}

	events, err := derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	startSeq := capsession.RunStartSeq(events)
	if startSeq == 0 {
		t.Fatal("expected a run start seq")
	}
	if got := capsession.FinishAfter(events, startSeq); got != nil {
		t.Fatalf("stale finish leaked into the new run: %+v", got)
	}

	if err := sessevents.Default.AppendRunFinish(ctx, sess, "a", capsession.RunFinishData{
		Status:  capsession.FinishBlocked,
		Summary: "cannot proceed",
	}); err != nil {
		t.Fatal(err)
	}
	events, err = derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	finish := capsession.FinishAfter(events, startSeq)
	if finish == nil || finish.Status != capsession.FinishBlocked {
		t.Fatalf("finish = %+v, want blocked", finish)
	}
}

func TestRunStartSeqIgnoresContinuations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := newRunStateSession(t)
	if err := sessevents.Default.AppendMessage(ctx, sess, "a", agentkit.EventUserMessage, agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "task"}},
	}); err != nil {
		t.Fatal(err)
	}
	events, err := derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	startSeq := capsession.RunStartSeq(events)

	// A continuation is its own event type, so it must not move the run start.
	if err := sessevents.Default.AppendTurnContinue(ctx, sess, "a", capsession.TurnContinueData{
		Segment: 1,
		Reason:  "no-tool-calls",
		Messages: []agentkit.ModelMessage{{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "keep going"}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	events, err = derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if got := capsession.RunStartSeq(events); got != startSeq {
		t.Fatalf("run start seq moved from %d to %d after a continuation", startSeq, got)
	}

	// The continuation is still model-visible.
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1].Content[0].Text != "keep going" {
		t.Fatalf("derived messages = %+v, want the continuation replayed", messages)
	}
}

func TestRepeatedToolCallsCountsConsecutiveTail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := newRunStateSession(t)
	appendCall := func(name, input string) {
		t.Helper()
		if err := sessevents.Default.AppendToolCall(ctx, sess, "a", agentkit.ToolCall{
			ID:    agentkit.ToolCallID(name),
			Name:  name,
			Input: []byte(input),
		}); err != nil {
			t.Fatal(err)
		}
	}

	appendCall("read", `{"path":"a.go"}`)
	// Same call three times, with key order shuffled to prove normalization.
	appendCall("read", `{"path":"b.go"}`)
	appendCall("read", `{ "path" : "b.go" }`)
	appendCall("read", `{"path":"b.go"}`)

	events, err := derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if got := capsession.RepeatedToolCalls(events, 0); got != 3 {
		t.Fatalf("repeats = %d, want 3", got)
	}

	appendCall("read", `{"path":"c.go"}`)
	events, err = derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if got := capsession.RepeatedToolCalls(events, 0); got != 1 {
		t.Fatalf("repeats after a different call = %d, want 1", got)
	}
}

func TestTotalUsageSumsAfterSeq(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := newRunStateSession(t)
	if err := sessevents.Default.AppendUsage(ctx, sess, "a", capsession.UsageData{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}); err != nil {
		t.Fatal(err)
	}
	events, err := derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	cutoff := capsession.LatestEventSeq(events)

	if err := sessevents.Default.AppendUsage(ctx, sess, "a", capsession.UsageData{InputTokens: 20, OutputTokens: 7, TotalTokens: 27}); err != nil {
		t.Fatal(err)
	}
	events, err = derive.ReadAllEvents(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if got := capsession.TotalUsage(events, 0).TotalTokens; got != 42 {
		t.Fatalf("total usage = %d, want 42", got)
	}
	if got := capsession.TotalUsage(events, cutoff).TotalTokens; got != 27 {
		t.Fatalf("usage after cutoff = %d, want 27", got)
	}
}
