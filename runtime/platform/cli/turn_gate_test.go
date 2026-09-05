package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
)

func TestCLIReceiveWaitsForTurnEnd(t *testing.T) {
	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	platform := p.(*Platform)
	platform.welcomed = true
	platform.input = NewInput(strings.NewReader("next\n"))

	platform.beginTurnWait()

	type result struct {
		event agentkit.MessageEvent
		err   error
	}
	done := make(chan result, 1)
	go func() {
		ev, err := platform.Receive(context.Background())
		done <- result{ev, err}
	}()

	select {
	case res := <-done:
		t.Fatalf("Receive returned early: %+v, err=%v", res.event, res.err)
	case <-time.After(50 * time.Millisecond):
	}

	platform.endTurnWait()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatal(res.err)
		}
		if textOf(res.event.Message) != "next" {
			t.Fatalf("message = %q, want next", textOf(res.event.Message))
		}
	case <-time.After(time.Second):
		t.Fatal("Receive did not finish after turn end")
	}
}

func TestCLIPermissionBypassesTurnWait(t *testing.T) {
	p, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	platform := p.(*Platform)
	platform.welcomed = true
	platform.input = NewInput(strings.NewReader("sqlite\n"))
	platform.beginTurnWait()

	platform.mu.Lock()
	platform.pending = &permissionPrompt{requestID: "perm1"}
	platform.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		event, err := platform.Receive(context.Background())
		if err != nil {
			done <- err
			return
		}
		if len(event.Reply) == 0 {
			done <- errMissingReply
			return
		}
		reply, err := rtpermission.DecodeReply(event.Reply)
		if err != nil {
			done <- err
			return
		}
		if reply.Text != "sqlite" {
			done <- errBadReply
			return
		}
		close(done)
	}()

	select {
	case err := <-done:
		if err == nil {
			return
		}
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("permission receive blocked on turn wait")
	}
}

var (
	errMissingReply = errTurnGate("missing permission reply")
	errBadReply     = errTurnGate("unexpected permission reply")
)

type errTurnGate string

func (e errTurnGate) Error() string { return string(e) }
