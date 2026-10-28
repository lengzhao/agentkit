package subagent

import (
	"context"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type cwdChildProbe struct {
	store agentkit.SessionStore
	ws    workspace.Service
	seen  chan string
}

func (cwdChildProbe) ID() agentkit.AgentID { return "cursor" }
func (p cwdChildProbe) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	// ACP entry points clear stale execution snapshots before preparing a turn.
	ctx, err := sessctx.Prepare(rctx.WithoutExecution(ctx), p.store, p.ws)
	if err != nil {
		return err
	}
	p.seen <- workspace.WorkingDirectory(ctx)
	return nil
}
func TestDelegationInheritsParentCwdSnapshot(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "async"}[async], func(t *testing.T) {
			s, store, completed := newLoopSpawner(t, async, "done")
			ws := rtworkspace.Static(t.TempDir())
			project := t.TempDir()
			channelDefault := t.TempDir()
			ctx := loopParentCtx()
			stack := sessbind.StackFromContext(ctx, ws)
			if err := stack.Set(ctx, store, sessbind.CwdBinding{}, sessbind.LevelChannel, channelDefault); err != nil {
				t.Fatal(err)
			}
			if err := store.(agentkit.SessionCwdStore).SetCwdBind(ctx, "cli:default", project); err != nil {
				t.Fatal(err)
			}
			ctx, err := sessctx.Prepare(ctx, store, ws)
			if err != nil {
				t.Fatal(err)
			}
			seen := make(chan string, 1)
			s.agents["cursor"] = cwdChildProbe{store: store, ws: ws, seen: seen}
			if _, err := s.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "inspect project"}); err != nil {
				t.Fatal(err)
			}
			select {
			case cwd := <-seen:
				if cwd != project {
					t.Fatalf("child cwd=%q want parent=%q", cwd, project)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("child did not run")
			}
			if async {
				select {
				case <-completed:
				case <-time.After(3 * time.Second):
					t.Fatal("async child did not finish")
				}
			}
		})
	}
}

func TestAsyncParentCapturePreservesCwd(t *testing.T) {
	parentDir := t.TempDir()
	ctx := rctx.WithConversation(context.Background(), "parent")
	ctx = rctx.WithExecution(ctx, rctx.Execution{SessionID: "parent", Cwd: parentDir, Epoch: "parent-context"})
	captured := captureParentContext(ctx, nil)
	detached := captured.asyncRunContext()
	if got := workspace.WorkingDirectory(detached); got != parentDir {
		t.Fatalf("async capture lost directory: %q", got)
	}
}
