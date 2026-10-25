package runner_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
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

func TestRunnerResolvesChannelAgentBind(t *testing.T) {
	t.Parallel()

	const sessionID = agentkit.SessionID("slack:C001")
	mem, err := sessstore.NewMemory(sessstore.MemoryConfig{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	store := mapSessionStore{
		sessions: map[agentkit.SessionID]agentkit.Session{sessionID: mem},
		binds:    map[agentkit.SessionID]agentkit.AgentID{},
	}
	ws, err := rtworkspace.NewTenant(rtworkspace.TenantConfig{
		Global:    t.TempDir(),
		LocalBase: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery := rctx.BuildDeliverySessionID("slack", "C001", "", "U1")
	env := rctx.ResolveEnvelope(deliveryUserEvent("slack", "U1", delivery, ""), rctx.DefaultRoutePolicy(agentkit.SessionScopeChannel))
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), env)
	if err := sessbind.SetGlobalAgentBind(ctx, ws, "global-agent"); err != nil {
		t.Fatal(err)
	}
	if err := sessbind.SetChannelAgentBind(ctx, store, ws, sessionID, "channel-agent"); err != nil {
		t.Fatal(err)
	}

	loop := &agentRecordingLoop{}
	evt := deliveryUserEvent("slack", "U1", rctx.BuildDeliverySessionID("slack", "C001", "", "U1"), "hi")
	evt.Envelope.AgentID = "request-agent"
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform:     &scriptedPlatform{events: []agentkit.MessageEvent{evt}},
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
	if loop.lastAgent != "channel-agent" {
		t.Fatalf("agent = %q, want channel-agent", loop.lastAgent)
	}
}
