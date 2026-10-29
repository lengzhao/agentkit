package runner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type slowSteerPlatform struct {
	eofPlatform
	started chan context.Context
	release chan struct{}
	done    chan struct{}
	calls   atomic.Int32
}

func (p *slowSteerPlatform) Send(ctx context.Context, _ agentkit.OutboundEvent) error {
	p.calls.Add(1)
	p.started <- ctx
	<-p.release
	close(p.done)
	return nil
}

func TestTerminalSteerAckDoesNotBlockExecution(t *testing.T) {
	p := &slowSteerPlatform{started: make(chan context.Context, 2), release: make(chan struct{}), done: make(chan struct{})}
	t.Cleanup(func() {
		close(p.release)
		select {
		case <-p.done:
		case <-time.After(time.Second):
			t.Error("platform sender did not finish after release")
		}
	})
	r := &Root{platform: p}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := agentkit.TurnEnvelope{Conversation: "lark:C1", Route: rctx.SessionRoute("lark", "lark:C1")}
	ack := r.turnSteerAck(ctx, env, agentkit.MessageEvent{Envelope: env})
	returned := make(chan struct{})
	go func() { ack(capsession.SteerOutcomeInjected); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("terminal ack blocked execution on platform I/O")
	}
	select {
	case sendCtx := <-p.started:
		if sendCtx.Err() != nil {
			t.Fatal("turn cancellation dropped terminal outcome")
		}
		if _, ok := sendCtx.Deadline(); !ok {
			t.Fatal("platform I/O has no timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("terminal outcome was not sent")
	}
	ack(capsession.SteerOutcomeNextTurn)
	if p.calls.Load() != 1 {
		t.Fatalf("duplicate terminal outcomes: %d", p.calls.Load())
	}
}
