package runner

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
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
func (l *inboundRoutingLoop) CancelAllInFlight(string)                               {}
func (l *inboundRoutingLoop) TryDeliverPermission(agentkit.MessageEvent) bool        { return false }
func (l *inboundRoutingLoop) SupersedePendingForInbound(agentkit.MessageEvent)       {}

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
			Route: session.SessionRoute("cli", string(sessionID)),
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

func TestSubagentCompleteWhileBusyUsesFollowUp(t *testing.T) {
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

	r.handleInbound(context.Background(), sched, subagentCompleteEvent(sessionID))

	loop.mu.Lock()
	defer loop.mu.Unlock()
	if len(loop.steers) != 0 {
		t.Fatalf("steer = %v, want none", loop.steers)
	}
	if len(loop.followUps) != 1 {
		t.Fatalf("follow-ups = %d, want 1", len(loop.followUps))
	}
	if !strings.Contains(loop.followUps[0], "subagent-complete") {
		t.Fatalf("follow-up = %q", loop.followUps[0])
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
			Route: session.SessionRoute("cli", string(sessionID)),
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
