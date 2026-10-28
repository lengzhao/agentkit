package runner

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type cwdLoop struct {
	agentkit.Loop
	seen chan string
}

func (l cwdLoop) Dispatch(ctx context.Context, _ agentkit.LoopRequest) error {
	l.seen <- workspace.WorkingDirectory(ctx)
	return nil
}

func TestRunnerPreparesCwdForAnyLoop(t *testing.T) {
	root := t.TempDir()
	a := t.TempDir()
	b := t.TempDir()
	ws := rtworkspace.Static(root)
	store, err := sessstore.NewStore(sessstore.StoreConfig{Dir: "sessions"}, sessstore.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 2)
	r := Root{loop: cwdLoop{seen: seen}, workspace: ws, sessionStore: store}
	var cmd agentkit.Command
	for _, c := range r.Commands() {
		if c.Name() == "cwd" {
			cmd = c
		}
	}
	if cmd == nil {
		t.Fatal("custom loop runner has no /cwd command")
	}
	for _, item := range []struct{ id, path string }{{"one", a}, {"two", b}} {
		ctx := rctx.WithConversation(context.Background(), item.id)
		if _, err := cmd.CommandExec(ctx, "use "+item.path); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		go func(id string) {
			done <- r.dispatch(context.Background(), agentkit.LoopRequest{Event: agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Conversation: id}}})
		}(id)
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]bool{<-seen: true, <-seen: true}
	if !got[a] || !got[b] {
		t.Fatalf("concurrent cwd snapshots: %v", got)
	}
}
