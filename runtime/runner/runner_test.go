package runner_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/runner"
)

// scriptedPlatform feeds a fixed set of events then reports EOF, and records
// everything the runner sends back.
type scriptedPlatform struct {
	events []agentkit.MessageEvent

	mu   sync.Mutex
	next int
	sent []agentkit.OutboundEvent
}

func (p *scriptedPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.next >= len(p.events) {
		return agentkit.MessageEvent{}, io.EOF
	}
	event := p.events[p.next]
	p.next++
	return event, nil
}

func (p *scriptedPlatform) Send(_ context.Context, event agentkit.OutboundEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, event)
	return nil
}

func (p *scriptedPlatform) sentEvents() []agentkit.OutboundEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]agentkit.OutboundEvent(nil), p.sent...)
}

// panickyLoop panics on the first dispatch and succeeds afterwards.
type panickyLoop struct {
	mu    sync.Mutex
	calls int
}

func (l *panickyLoop) Dispatch(_ context.Context, _ agentkit.LoopRequest) error {
	l.mu.Lock()
	l.calls++
	first := l.calls == 1
	l.mu.Unlock()
	if first {
		var m map[string]string
		m["boom"] = "nil map write" // panics on purpose
	}
	return nil
}

func (l *panickyLoop) Steer(context.Context, agentkit.ModelMessage) error    { return nil }
func (l *panickyLoop) FollowUp(context.Context, agentkit.ModelMessage) error { return nil }

func (l *panickyLoop) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func userEvent(sessionID agentkit.SessionID, text string) agentkit.MessageEvent {
	return agentkit.MessageEvent{
		SessionID: sessionID,
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: text}},
		},
	}
}

// TestRunSurvivesPanickingTurn is the daemon guarantee: one bad turn must not
// take down a process that is meant to run for days.
func TestRunSurvivesPanickingTurn(t *testing.T) {
	t.Parallel()

	platform := &scriptedPlatform{events: []agentkit.MessageEvent{
		userEvent("s:1", "this one panics"),
		userEvent("s:2", "this one works"),
	}}
	loop := &panickyLoop{}
	root, err := runner.New(runner.Config{}, runner.Deps{Platform: platform, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}

	if err := root.Run(context.Background(), nil); err != nil {
		t.Fatalf("run returned %v, want nil: a panicking turn must not end the process", err)
	}
	if got := loop.count(); got != 2 {
		t.Fatalf("dispatch calls = %d, want 2: the second event must still be served", got)
	}

	// The panic is reported on the failing session's error channel, not swallowed.
	var reported string
	for _, event := range platform.sentEvents() {
		if event.Type != "error" {
			continue
		}
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatalf("decode error event: %v", err)
		}
		if event.SessionID != "s:1" {
			t.Fatalf("error reported on session %q, want s:1", event.SessionID)
		}
		reported = payload.Error
	}
	if !strings.Contains(reported, "panicked") {
		t.Fatalf("error event = %q, want it to mention the panic", reported)
	}
}

// errorLoop always fails, without panicking.
type errorLoop struct{ err error }

func (l errorLoop) Dispatch(context.Context, agentkit.LoopRequest) error  { return l.err }
func (l errorLoop) Steer(context.Context, agentkit.ModelMessage) error    { return nil }
func (l errorLoop) FollowUp(context.Context, agentkit.ModelMessage) error { return nil }

func TestRunKeepsServingAfterTurnError(t *testing.T) {
	t.Parallel()

	platform := &scriptedPlatform{events: []agentkit.MessageEvent{
		userEvent("s:1", "fails"),
		userEvent("s:2", "also fails"),
	}}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform: platform,
		Loop:     errorLoop{err: io.ErrUnexpectedEOF},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Run(context.Background(), nil); err != nil {
		t.Fatalf("run returned %v, want nil", err)
	}
	errors := 0
	for _, event := range platform.sentEvents() {
		if event.Type == "error" {
			errors++
		}
	}
	if errors != 2 {
		t.Fatalf("error events = %d, want 2", errors)
	}
}
