package loop_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtloop "github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessctx"
	"github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

type cwdCallbackAgent struct{ run func(context.Context) error }

func (cwdCallbackAgent) ID() agentkit.AgentID                                      { return "cwd-test" }
func (a cwdCallbackAgent) RunTurn(ctx context.Context, _ agentkit.TurnInput) error { return a.run(ctx) }

func TestFollowUpTurnRefreshesCwd(t *testing.T) {
	root := t.TempDir()
	first := t.TempDir()
	second := t.TempDir()
	ws := rtworkspace.Static(root)
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "session"})
	store := sessstore.NewStaticStore(mem)
	base := rctx.WithConversation(context.Background(), "session")
	_ = store.SetCwdBind(base, "session", first)
	ctx, err := sessctx.Prepare(base, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	var loop agentkit.Loop
	var dirs []string
	ag := cwdCallbackAgent{run: func(ctx context.Context) error {
		dirs = append(dirs, workspace.WorkingDirectory(ctx))
		if len(dirs) == 1 {
			if err := store.SetCwdBind(ctx, "session", second); err != nil {
				return err
			}
			return loop.FollowUp(ctx, agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "next"}}})
		}
		return nil
	}}
	loop, err = rtloop.New(rtloop.Config{}, rtloop.Deps{Agents: []agentkit.Agent{ag}, SessionStore: store, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.CancelAllInFlight("test end")
	req := agentkit.LoopRequest{Event: agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Conversation: "session"}}}
	if err := loop.Dispatch(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 || dirs[0] != first || dirs[1] != second {
		t.Fatalf("turn directories: %v", dirs)
	}
}

func TestFirstTurnPreservesPreparedDirectory(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	first, second := t.TempDir(), t.TempDir()
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "session"})
	store := sessstore.NewStaticStore(mem)
	ctx := rctx.WithConversation(context.Background(), "session")
	if err := store.SetCwdBind(ctx, "session", first); err != nil {
		t.Fatal(err)
	}
	ctx, err := sessctx.Prepare(ctx, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a binding update after attachments have consumed the snapshot.
	if err = store.SetCwdBind(ctx, "session", second); err != nil {
		t.Fatal(err)
	}
	ag := cwdCallbackAgent{run: func(ctx context.Context) error {
		if got := workspace.WorkingDirectory(ctx); got != first {
			t.Errorf("first turn cwd = %s, want fixed snapshot %s", got, first)
		}
		return nil
	}}
	loop, err := rtloop.New(rtloop.Config{}, rtloop.Deps{Agents: []agentkit.Agent{ag}, SessionStore: store, Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.CancelAllInFlight("test end")
	if err = loop.Dispatch(ctx, agentkit.LoopRequest{Event: agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Conversation: "session"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerSnapshotServicesSurviveLoopWithoutOptionalDeps(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	first, second := t.TempDir(), t.TempDir()
	mem, _ := sessstore.NewMemory(sessstore.MemoryConfig{ID: "session"})
	store := sessstore.NewStaticStore(mem)
	base := rctx.WithConversation(context.Background(), "session")
	if err := store.SetCwdBind(base, "session", first); err != nil {
		t.Fatal(err)
	}
	ctx, err := sessctx.Prepare(base, store, ws)
	if err != nil {
		t.Fatal(err)
	}
	var loop agentkit.Loop
	var dirs []string
	ag := cwdCallbackAgent{run: func(ctx context.Context) error {
		dirs = append(dirs, workspace.WorkingDirectory(ctx))
		if len(dirs) == 1 {
			if err := store.SetCwdBind(ctx, "session", second); err != nil {
				return err
			}
			return loop.FollowUp(ctx, agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "next"}}})
		}
		return nil
	}}
	loop, err = rtloop.New(rtloop.Config{}, rtloop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.CancelAllInFlight("test end")
	if err = loop.Dispatch(ctx, agentkit.LoopRequest{Event: agentkit.MessageEvent{Envelope: agentkit.TurnEnvelope{Conversation: "session"}}}); err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 2 || dirs[0] != first || dirs[1] != second {
		t.Fatalf("follow-up directories = %v", dirs)
	}
}
