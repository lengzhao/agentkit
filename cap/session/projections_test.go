package session_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
)

func event(t *testing.T, seq agentkit.EventSeq, typ agentkit.EventType, data any) agentkit.SessionEvent {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return agentkit.SessionEvent{Seq: seq, Type: typ, Data: raw}
}

func TestRunStateFromEvents(t *testing.T) {
	t.Parallel()

	events := []agentkit.SessionEvent{
		event(t, 1, agentkit.EventUserMessage, agentkit.ModelMessage{Role: "user"}),
		event(t, 2, agentkit.EventTodoUpdate, capsession.TodoUpdateData{Items: []capsession.Todo{
			{ID: "1", Title: "done thing", Status: capsession.TodoDone},
			{ID: "2", Title: "pending thing", Status: capsession.TodoPending},
		}}),
		event(t, 3, agentkit.EventUsage, capsession.UsageData{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}),
		event(t, 4, agentkit.EventUsage, capsession.UsageData{InputTokens: 20, OutputTokens: 5, TotalTokens: 25}),
	}

	state := capsession.RunStateFromEvents(events)
	if state.StartSeq != 1 {
		t.Fatalf("StartSeq = %d, want 1", state.StartSeq)
	}
	if len(state.Todos) != 2 || len(state.Pending) != 1 || state.Pending[0].ID != "2" {
		t.Fatalf("todos/pending = %+v / %+v", state.Todos, state.Pending)
	}
	if state.Usage.TotalTokens != 40 {
		t.Fatalf("usage total = %d, want 40", state.Usage.TotalTokens)
	}
	if state.Context != 20 {
		t.Fatalf("context = %d, want latest input 20", state.Context)
	}
	if state.Finish != nil {
		t.Fatalf("finish = %+v, want nil", state.Finish)
	}
}

func TestFinishAfterRespectsSeq(t *testing.T) {
	t.Parallel()

	events := []agentkit.SessionEvent{
		event(t, 1, agentkit.EventRunFinish, capsession.RunFinishData{Status: capsession.FinishCompleted, Summary: "old"}),
		event(t, 2, agentkit.EventUserMessage, agentkit.ModelMessage{Role: "user"}),
	}
	if got := capsession.FinishAfter(events, capsession.RunStartSeq(events)); got != nil {
		t.Fatalf("stale finish leaked into the new run: %+v", got)
	}
	events = append(events, event(t, 3, agentkit.EventRunFinish, capsession.RunFinishData{Status: capsession.FinishBlocked}))
	got := capsession.FinishAfter(events, 2)
	if got == nil || got.Status != capsession.FinishBlocked {
		t.Fatalf("finish = %+v, want blocked", got)
	}
}

func TestRepeatedToolCallsNormalizesInput(t *testing.T) {
	t.Parallel()

	call := func(input string) agentkit.ToolCall {
		return agentkit.ToolCall{ID: "c", Name: "read", Input: json.RawMessage(input)}
	}
	events := []agentkit.SessionEvent{
		event(t, 1, agentkit.EventToolCall, call(`{"path":"a.go"}`)),
		event(t, 2, agentkit.EventToolCall, call(`{"path":"b.go"}`)),
		event(t, 3, agentkit.EventToolCall, call(`{ "path" : "b.go" }`)),
		event(t, 4, agentkit.EventToolCall, call(`{"path":"b.go"}`)),
	}
	if got := capsession.RepeatedToolCalls(events, 0); got != 3 {
		t.Fatalf("repeats = %d, want 3", got)
	}
}

func TestLastAssistantTextAndStepCount(t *testing.T) {
	t.Parallel()

	events := []agentkit.SessionEvent{
		event(t, 1, agentkit.EventAssistantMessage, agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "first"}}}),
		event(t, 2, agentkit.EventStepEnd, capsession.StepEndData{Step: 0}),
		event(t, 3, agentkit.EventAssistantMessage, agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "second"}}}),
		event(t, 4, agentkit.EventStepEnd, capsession.StepEndData{Step: 1}),
	}
	if got := capsession.LastAssistantText(events, 0); got != "second" {
		t.Fatalf("last assistant text = %q", got)
	}
	if got := capsession.StepCount(events, 2); got != 1 {
		t.Fatalf("steps after 2 = %d, want 1", got)
	}
	if got := capsession.LatestEventSeq(events); got != 4 {
		t.Fatalf("latest seq = %d, want 4", got)
	}
}

func TestEstimateLogicalCharsCountsAttachmentRefBySource(t *testing.T) {
	t.Parallel()

	msg := agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: agentkit.ContentTypeAttachmentRef, Source: "upload/big.log"}},
	}
	if got := capsession.EstimateLogicalChars(msg); got != len("user")+len("upload/big.log") {
		t.Fatalf("logical chars = %d", got)
	}
}

func TestSumLogicalCharsFromEventsPrefersRecordedMetadata(t *testing.T) {
	t.Parallel()

	ev := event(t, 1, agentkit.EventUserMessage, agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}})
	ev.AgentID = "a"
	ev.Metadata = map[string]any{capsession.MetadataLogicalChars: 3_000_000}
	other := event(t, 2, agentkit.EventUserMessage, agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "other agent"}}})
	other.AgentID = "b"

	if got := capsession.SumLogicalCharsFromEvents([]agentkit.SessionEvent{ev, other}, "a"); got != 3_000_000 {
		t.Fatalf("sum = %d, want recorded 3000000", got)
	}
}

func TestFlattenTextParts(t *testing.T) {
	t.Parallel()

	parts := []agentkit.ContentPart{
		{Type: "text", Text: " hello "},
		{Type: "image", URL: "data:..."},
		{Text: "world"},
		{Type: "text", Text: "  "},
	}
	if got := capsession.FlattenTextParts(parts, " "); got != "hello world" {
		t.Fatalf("flatten = %q", got)
	}
}

type fakeActiveStore struct {
	active agentkit.SessionID
	err    error
}

func (f fakeActiveStore) Get(context.Context, agentkit.SessionID) (agentkit.Session, error) {
	return nil, nil
}

func (f fakeActiveStore) ActiveSession(context.Context, agentkit.SessionID) (agentkit.SessionID, error) {
	return f.active, f.err
}

func (f fakeActiveStore) SetActiveSession(context.Context, agentkit.SessionID, agentkit.SessionID) error {
	return nil
}

func TestResolveActiveSessionID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if got, err := capsession.ResolveActiveSessionID(ctx, fakeActiveStore{}, ""); err != nil || got != "" {
		t.Fatalf("empty key = %q, %v", got, err)
	}
	if got, err := capsession.ResolveActiveSessionID(ctx, fakeActiveStore{active: "s2"}, "entry"); err != nil || got != "s2" {
		t.Fatalf("mapped = %q, %v", got, err)
	}
	if got, err := capsession.ResolveActiveSessionID(ctx, fakeActiveStore{}, "entry"); err != nil || got != "entry" {
		t.Fatalf("unmapped = %q, %v", got, err)
	}
}
