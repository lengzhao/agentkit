package runner_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/runner"
	"github.com/lengzhao/agentkit/runtime/session/sessbind"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

func TestRunnerResolvesGlobalAgentBind(t *testing.T) {
	t.Parallel()

	const sessionID = agentkit.SessionID("cli:test")
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	store := mapSessionStore{
		sessions: map[agentkit.SessionID]agentkit.Session{sessionID: mem},
	}
	ws := rtworkspace.Static(t.TempDir())
	ctx := context.Background()
	if err := sessbind.SetGlobalAgentBind(ctx, ws, "reviewer"); err != nil {
		t.Fatal(err)
	}

	loop := &agentRecordingLoop{}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform:     &scriptedPlatform{events: []agentkit.MessageEvent{userEvent(sessionID, "hi")}},
		Loop:         loop,
		SessionStore: store,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if loop.lastAgent != "reviewer" {
		t.Fatalf("agent = %q, want reviewer", loop.lastAgent)
	}
}
