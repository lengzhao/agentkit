package runner_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
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
		panic("boom")
	}
	return nil
}

func (l *panickyLoop) Steer(context.Context, agentkit.ModelMessage) error    { return nil }
func (l *panickyLoop) FollowUp(context.Context, agentkit.ModelMessage) error { return nil }
func (l *panickyLoop) TryDeliverPermission(agentkit.MessageEvent) bool      { return false }
func (l *panickyLoop) SupersedePendingForInbound(agentkit.MessageEvent)       {}

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
func (l errorLoop) TryDeliverPermission(agentkit.MessageEvent) bool        { return false }
func (l errorLoop) SupersedePendingForInbound(agentkit.MessageEvent)       {}

// permissionLoop blocks the first dispatch until closed, and records permission
// deliveries via TryDeliverPermission.
type permissionLoop struct {
	mu sync.Mutex

	turnBlocked chan struct{}
	releaseTurn chan struct{}
	delivered   int
}

func (l *permissionLoop) Dispatch(_ context.Context, _ agentkit.LoopRequest) error {
	l.mu.Lock()
	if l.turnBlocked == nil {
		l.mu.Unlock()
		return nil
	}
	blocked := l.turnBlocked
	release := l.releaseTurn
	l.turnBlocked = nil
	l.mu.Unlock()

	close(blocked)
	select {
	case <-release:
	case <-time.After(5 * time.Second):
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (l *permissionLoop) Steer(context.Context, agentkit.ModelMessage) error    { return nil }
func (l *permissionLoop) FollowUp(context.Context, agentkit.ModelMessage) error { return nil }

func (l *permissionLoop) TryDeliverPermission(event agentkit.MessageEvent) bool {
	if len(event.Reply) == 0 {
		return false
	}
	l.mu.Lock()
	l.delivered++
	l.mu.Unlock()
	return true
}

func (l *permissionLoop) SupersedePendingForInbound(agentkit.MessageEvent) {}

func (l *permissionLoop) deliveredCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.delivered
}

// stagedPermissionPlatform serves one user turn, then waits until the turn is
// blocked before offering a permission reply.
type stagedPermissionPlatform struct {
	turnBlocked chan struct{}
	replySent   chan struct{}

	mu       sync.Mutex
	stage    int
	received int
}

func (p *stagedPermissionPlatform) Receive(ctx context.Context) (agentkit.MessageEvent, error) {
	p.mu.Lock()
	stage := p.stage
	p.stage++
	p.received++
	p.mu.Unlock()

	switch stage {
	case 0:
		return userEvent("s:1", "start turn"), nil
	case 1:
		select {
		case <-p.turnBlocked:
		case <-ctx.Done():
			return agentkit.MessageEvent{}, ctx.Err()
		}
		select {
		case p.replySent <- struct{}{}:
		default:
		}
		return agentkit.MessageEvent{
			SessionID: "s:1",
			Reply: permission.MarshalReply(permission.Reply{
				RequestID: "perm1",
				Text:      "yes",
			}),
		}, nil
	default:
		return agentkit.MessageEvent{}, io.EOF
	}
}

func (p *stagedPermissionPlatform) Send(context.Context, agentkit.OutboundEvent) error {
	return nil
}

// TestReceiveDeliversPermissionWhileTurnBlocked is the P1 guarantee: the
// receive loop must not hold a concurrency slot, so permission replies can
// still be read and delivered while a turn waits on pending input.
func TestReceiveDeliversPermissionWhileTurnBlocked(t *testing.T) {
	t.Parallel()

	turnBlocked := make(chan struct{})
	releaseTurn := make(chan struct{})
	replySent := make(chan struct{}, 1)
	loop := &permissionLoop{
		turnBlocked: turnBlocked,
		releaseTurn: releaseTurn,
	}
	platform := &stagedPermissionPlatform{
		turnBlocked: turnBlocked,
		replySent:   replySent,
	}

	root, err := runner.New(runner.Config{}, runner.Deps{Platform: platform, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- root.Run(context.Background(), nil) }()

	select {
	case <-turnBlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not start blocking")
	}

	select {
	case <-replySent:
	case <-time.After(2 * time.Second):
		t.Fatal("permission reply was not received while turn was blocked")
	}
	if got := loop.deliveredCount(); got != 1 {
		t.Fatalf("delivered = %d, want 1", got)
	}

	close(releaseTurn)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not finish after unblocking turn")
	}
}

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
