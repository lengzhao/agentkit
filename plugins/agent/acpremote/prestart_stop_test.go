package acpremote

import (
	"context"
	"errors"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// A /stop that lands after Loop marks the session busy but before the remote
// agent registers its step must abort the turn instead of spawning a subprocess.
// Loop's Cancel only sets the reason here; it cannot cancel a step that does not
// exist yet, which is exactly the window this guards.
func TestPreStartStopAbortsBeforeSpawn(t *testing.T) {
	rt, err := New(Config{
		ID: "cursor",
		// A path that cannot exist: reaching it means we already failed the
		// expectation, and the error kind tells the two outcomes apart.
		Command: []string{"/nonexistent/agentkit-acp-prestart-probe"},
	}, Deps{
		Workspace:     &stubWorkspace{},
		FS:            bindTestFS(t, t.TempDir()),
		SessionStore:  staticStore{sess: mustMemSession(t)},
		SessionEvents: mustEvents(t),
		Telemetry:     mustToolkit(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctrl := loop.NewControl()
	defer ctrl.Stop()
	if err := ctrl.Cancel(context.Background(), "/stop"); err != nil {
		t.Fatal(err)
	}

	ctx := agenttest.TurnContext("prestart-stop", "cursor")
	ctx = context.WithValue(ctx, agentkit.KeySessionControl, ctrl)
	err = rt.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "work"}}},
		Emit:    func(context.Context, agentkit.OutboundEvent) error { return nil },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-start /stop must abort the turn; got %v", err)
	}
	// The reason must be consumed, not left to cancel the next turn too.
	if leftover := ctrl.PopCancelReason(); leftover != "" {
		t.Fatalf("cancel reason leaked into the next turn: %q", leftover)
	}
}

type staticStore struct{ sess agentkit.Session }

func (s staticStore) Get(_ context.Context, _ agentkit.SessionID) (agentkit.Session, error) {
	return s.sess, nil
}

func mustMemSession(t *testing.T) agentkit.Session {
	t.Helper()
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: "prestart-stop"})
	if err != nil {
		t.Fatal(err)
	}
	return mem
}
