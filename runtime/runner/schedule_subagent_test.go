package runner

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type eofPlatform struct{}

func (eofPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	return agentkit.MessageEvent{}, io.EOF
}
func (eofPlatform) Send(context.Context, agentkit.OutboundEvent) error { return nil }

type inboundRoutingLoop struct {
	mu        sync.Mutex
	busy      map[agentkit.SessionID]bool
	followUps []string
	steers    []string
}

func (l *inboundRoutingLoop) Dispatch(context.Context, agentkit.LoopRequest) error { return nil }
func (l *inboundRoutingLoop) Cancel(context.Context, string) error                 { return nil }
func (l *inboundRoutingLoop) CancelAllInFlight(string)                             {}
func (l *inboundRoutingLoop) TryDeliverPermission(agentkit.MessageEvent) bool      { return false }
func (l *inboundRoutingLoop) SupersedePendingForInbound(agentkit.MessageEvent)     {}

func (l *inboundRoutingLoop) Steer(_ context.Context, msg agentkit.ModelMessage) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.steers = append(l.steers, textPart(msg))
	return nil
}

func (l *inboundRoutingLoop) FollowUp(_ context.Context, msg agentkit.ModelMessage) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.followUps = append(l.followUps, textPart(msg))
	return nil
}

func (l *inboundRoutingLoop) IsSessionBusy(id agentkit.SessionID) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.busy[id]
}

func textPart(msg agentkit.ModelMessage) string {
	for _, part := range msg.Content {
		if part.Type == "text" {
			return part.Text
		}
	}
	return ""
}

func subagentCompleteEvent(sessionID agentkit.SessionID) agentkit.MessageEvent {
	return agentkit.MessageEvent{
		PlatformID: "cli",
		Envelope: agentkit.TurnEnvelope{
			Route: rctx.SessionRoute("cli", string(sessionID)),
		},
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "[subagent-complete agent=cursor job=sub:1 status=failed]\nerr"}},
		},
		Metadata: map[string]any{
			"subagent_complete": true,
		},
	}
}

func TestSubagentCompleteWhileBusyQueuesNewTurn(t *testing.T) {
	t.Parallel()

	sessionID := agentkit.SessionID("cli:default")
	loop := &inboundRoutingLoop{
		busy: map[agentkit.SessionID]bool{sessionID: true},
	}
	root, err := New(Config{}, Deps{Platform: eofPlatform{}, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*Root)

	var submitted []agentkit.LoopRequest
	var submitMu sync.Mutex
	sched := newScheduler(1, func(_ context.Context, req agentkit.LoopRequest) error {
		submitMu.Lock()
		submitted = append(submitted, req)
		submitMu.Unlock()
		return nil
	}, nil)

	r.handleInbound(context.Background(), sched, subagentCompleteEvent(sessionID))

	// Async subagent completion must NOT steer into the running turn and must NOT
	// use the in-loop FollowUp queue; it is a new logical turn routed through the
	// scheduler's per-session pending queue (runs as the next Run after the
	// current turn ends). The scheduler dispatch is async, so wait for it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		submitMu.Lock()
		n := len(submitted)
		submitMu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	loop.mu.Lock()
	defer loop.mu.Unlock()
	if len(loop.steers) != 0 {
		t.Fatalf("steer = %v, want none", loop.steers)
	}
	if len(loop.followUps) != 0 {
		t.Fatalf("follow-ups = %v, want none (subagent-complete routes via scheduler)", loop.followUps)
	}
	submitMu.Lock()
	defer submitMu.Unlock()
	if len(submitted) != 1 {
		t.Fatalf("submitted turns = %d, want 1", len(submitted))
	}
	if !strings.Contains(textPart(submitted[0].Event.Message), "subagent-complete") {
		t.Fatalf("submitted turn = %q, want subagent-complete", textPart(submitted[0].Event.Message))
	}
}

func TestBusyUserMessageStillSteers(t *testing.T) {
	t.Parallel()

	sessionID := agentkit.SessionID("cli:default")
	loop := &inboundRoutingLoop{
		busy: map[agentkit.SessionID]bool{sessionID: true},
	}
	root, err := New(Config{}, Deps{Platform: eofPlatform{}, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	r := root.(*Root)
	sched := newScheduler(1, func(context.Context, agentkit.LoopRequest) error { return nil }, nil)

	r.handleInbound(context.Background(), sched, agentkit.MessageEvent{
		PlatformID: "cli",
		Envelope: agentkit.TurnEnvelope{
			Route: rctx.SessionRoute("cli", string(sessionID)),
		},
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "interrupt"}},
		},
	})

	loop.mu.Lock()
	defer loop.mu.Unlock()
	if len(loop.steers) != 1 {
		t.Fatalf("steers = %d, want 1", len(loop.steers))
	}
	if len(loop.followUps) != 0 {
		t.Fatalf("follow-ups = %v, want none", loop.followUps)
	}
}
