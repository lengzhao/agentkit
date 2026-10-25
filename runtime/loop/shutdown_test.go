package loop

import (
	"bytes"
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

func TestControlStepCanFinishAfterStop(t *testing.T) {
	c := NewControl()
	ctx, end := c.BeginStep(context.Background())
	c.Stop()
	end()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("step not cancelled")
	}
	c.Stop()
	ctx, end = c.BeginStep(context.Background())
	defer end()
	if ctx.Err() == nil {
		t.Fatal("step begun after shutdown must be cancelled")
	}
}

func TestControlConcurrentStopAndCalls(t *testing.T) {
	c := NewControl()
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 20 {
				_ = c.Steer(context.Background(), agentkit.ModelMessage{})
				_, end := c.BeginStep(context.Background())
				end()
			}
		})
	}
	wg.Go(c.Stop)
	wg.Wait()
	c.Stop()
}

func TestStoppedControlRejectsPermissionRegistration(t *testing.T) {
	c := NewControl()
	c.Stop()
	if _, err := c.registerPermissionPending(permission.Request{ID: "after-stop"}, permission.Capability{}); err == nil {
		t.Fatal("permission registration after shutdown must fail")
	}
}

func TestControlLookupDoesNotLeakOwners(t *testing.T) {
	count := func() int {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		return bytes.Count(buf[:n], []byte(".(*Control).loop("))
	}
	before := count()
	l := &Default{}
	first := l.controlFor("same")
	for range 100 {
		if l.controlFor("same") != first {
			t.Fatal("control was replaced")
		}
	}
	runtime.Gosched()
	if got := count() - before; got > 1 {
		t.Errorf("created %d owners for one session", got)
	}
	_ = l.Stop(context.Background())
}

func TestControlStopReleasesPermissionWaiter(t *testing.T) {
	c := NewControl()
	req := permission.Request{ID: "pending-at-stop"}
	pending, err := c.registerPermissionPending(req, permission.Capability{})
	if err != nil {
		t.Fatal(err)
	}
	c.Stop()
	result, err := c.waitPermissionReply(context.Background(), req, pending, permission.Capability{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != permission.OutcomeCancelled {
		t.Fatalf("outcome = %s, want cancelled", result.Outcome)
	}
}

func TestControlReplyConcurrentWithStop(t *testing.T) {
	for range 100 {
		c := NewControl()
		_, err := c.registerPermissionPending(permission.Request{ID: "pending"}, permission.Capability{})
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() { c.DeliverPermissionReply("session", permission.Reply{RequestID: "pending"}) })
		wg.Go(c.Stop)
		wg.Wait()
	}
}

func TestLoopStopConcurrentWithControlCreation(t *testing.T) {
	l := &Default{}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { l.controlFor("existing"); l.controlFor("new") })
	}
	wg.Go(func() { _ = l.Stop(context.Background()) })
	wg.Wait()
	for _, session := range []agentkit.SessionID{"existing", "new", "after-stop"} {
		c := l.controlFor(session)
		select {
		case <-c.done:
		default:
			t.Fatalf("session %s still has a live owner after Stop", session)
		}
	}
	if err := l.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
