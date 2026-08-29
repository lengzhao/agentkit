package runner_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/runner"
	"github.com/lengzhao/agentkit/runtime/session"
)

type agentRecordingLoop struct {
	lastAgent        agentkit.AgentID
	lastSession      agentkit.SessionID
	lastStoreSession agentkit.SessionID
}

func (l *agentRecordingLoop) Dispatch(_ context.Context, req agentkit.LoopRequest) error {
	l.lastAgent = req.Event.AgentID
	l.lastSession = req.Event.SessionID
	l.lastStoreSession = req.StoreSessionID
	return nil
}

func (l *agentRecordingLoop) Steer(context.Context, agentkit.ModelMessage) error    { return nil }
func (l *agentRecordingLoop) FollowUp(context.Context, agentkit.ModelMessage) error { return nil }
func (l *agentRecordingLoop) TryDeliverPermission(agentkit.MessageEvent) bool       { return false }
func (l *agentRecordingLoop) SupersedePendingForInbound(agentkit.MessageEvent)      {}

type mapSessionStore struct {
	sessions map[agentkit.SessionID]agentkit.Session
	binds    map[agentkit.SessionID]agentkit.AgentID
	active   map[agentkit.SessionID]agentkit.SessionID
}

func (s mapSessionStore) Get(_ context.Context, id agentkit.SessionID) (agentkit.Session, error) {
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return sess, nil
}

func (s mapSessionStore) AgentBind(_ context.Context, id agentkit.SessionID) (agentkit.AgentID, error) {
	return s.binds[id], nil
}

func (s mapSessionStore) SetAgentBind(_ context.Context, id agentkit.SessionID, agent agentkit.AgentID) error {
	s.binds[id] = agent
	return nil
}

func (s mapSessionStore) ActiveSession(_ context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	if active, ok := s.sessions[id]; ok && active != nil {
		return id, nil
	}
	if active, ok := s.active[id]; ok {
		return active, nil
	}
	return id, nil
}

func (s mapSessionStore) SetActiveSession(_ context.Context, id, active agentkit.SessionID) error {
	s.active[id] = active
	return nil
}

type tenantScopedActiveStore struct {
	mapSessionStore
}

func (s tenantScopedActiveStore) ActiveSession(ctx context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	effective, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	if effective == "" {
		return id, nil
	}
	return s.mapSessionStore.ActiveSession(ctx, id)
}

func TestRunnerResolvesLogicalStoreSessionRequiresInboundContext(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("chat-api", "default_channel", "conv_test", "")
	logical := agentkit.SessionID(string(delivery) + ":new:20260829")
	mem, err := session.NewMemory(session.MemoryConfig{ID: logical})
	if err != nil {
		t.Fatal(err)
	}
	store := tenantScopedActiveStore{
		mapSessionStore: mapSessionStore{
			sessions: map[agentkit.SessionID]agentkit.Session{logical: mem},
			active:   map[agentkit.SessionID]agentkit.SessionID{delivery: logical},
		},
	}

	loop := &agentRecordingLoop{}
	event := userEvent(delivery, "today")
	event.PlatformID = "chat-api"
	root, err := runner.New(runner.Config{SessionScope: "channel"}, runner.Deps{
		Platform:     &scriptedPlatform{events: []agentkit.MessageEvent{event}},
		Loop:         loop,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if loop.lastStoreSession != logical {
		t.Fatalf("store session = %q, want %q", loop.lastStoreSession, logical)
	}
}

func TestRunnerResolvesLogicalStoreSessionFromFixedDelivery(t *testing.T) {
	t.Parallel()

	delivery := session.BuildDeliverySessionID("slack", "C001", "123", "U111")
	entry := session.ActiveSessionEntryKey("slack", delivery, session.ScopeChannel, "U111")
	logical := agentkit.SessionID(string(entry) + ":new:20260829")
	mem, err := session.NewMemory(session.MemoryConfig{ID: logical})
	if err != nil {
		t.Fatal(err)
	}
	store := mapSessionStore{
		sessions: map[agentkit.SessionID]agentkit.Session{logical: mem},
		active:   map[agentkit.SessionID]agentkit.SessionID{entry: logical},
	}

	loop := &agentRecordingLoop{}
	root, err := runner.New(runner.Config{SessionScope: "channel"}, runner.Deps{
		Platform:     &scriptedPlatform{events: []agentkit.MessageEvent{userEvent(delivery, "hi")}},
		Loop:         loop,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if loop.lastSession != "slack:C001" {
		t.Fatalf("effective session = %q, want slack:C001", loop.lastSession)
	}
	if loop.lastStoreSession != logical {
		t.Fatalf("store session = %q, want %q", loop.lastStoreSession, logical)
	}
}

func TestRunnerResolvesLogicalStoreSessionFromStableSlackDM(t *testing.T) {
	t.Parallel()

	stable := session.BuildDeliverySessionID("slack", "D0AK8MAHW22", "", "U02LNUW8KV5")
	entry := session.ActiveSessionEntryKey("slack", stable, session.ScopeChannel, "U02LNUW8KV5")
	logical := agentkit.SessionID(string(entry) + ":new:20260829")
	mem, err := session.NewMemory(session.MemoryConfig{ID: logical})
	if err != nil {
		t.Fatal(err)
	}
	store := mapSessionStore{
		sessions: map[agentkit.SessionID]agentkit.Session{logical: mem},
		active:   map[agentkit.SessionID]agentkit.SessionID{entry: logical},
	}

	loop := &agentRecordingLoop{}
	root, err := runner.New(runner.Config{SessionScope: "channel"}, runner.Deps{
		Platform: &scriptedPlatform{events: []agentkit.MessageEvent{
			userEvent(stable, "hi"),
		}},
		Loop:         loop,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if loop.lastStoreSession != logical {
		t.Fatalf("store session = %q, want %q", loop.lastStoreSession, logical)
	}
}

func TestRunnerAgentBindCacheUsesStoreBind(t *testing.T) {
	t.Parallel()

	const sessionID = agentkit.SessionID("cli:test")
	mem, err := session.NewMemory(session.MemoryConfig{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	store := mapSessionStore{
		sessions: map[agentkit.SessionID]agentkit.Session{sessionID: mem},
		binds:    map[agentkit.SessionID]agentkit.AgentID{sessionID: "reviewer"},
	}

	loop := &agentRecordingLoop{}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform: &scriptedPlatform{events: []agentkit.MessageEvent{
			userEvent(sessionID, "first"),
			userEvent(sessionID, "second"),
		}},
		Loop:         loop,
		SessionStore: store,
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

	store.binds[sessionID] = "assistant"
	loop2 := &agentRecordingLoop{}
	root2, err := runner.New(runner.Config{}, runner.Deps{
		Platform:     &scriptedPlatform{events: []agentkit.MessageEvent{userEvent(sessionID, "third")}},
		Loop:         loop2,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := root2.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if loop2.lastAgent != "assistant" {
		t.Fatalf("agent = %q, want assistant", loop2.lastAgent)
	}
}

func TestRunnerResolvesSessionAgentBind(t *testing.T) {
	t.Parallel()

	const sessionID = agentkit.SessionID("cli:test")
	mem, err := session.NewMemory(session.MemoryConfig{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	store := mapSessionStore{
		sessions: map[agentkit.SessionID]agentkit.Session{sessionID: mem},
		binds:    map[agentkit.SessionID]agentkit.AgentID{sessionID: "reviewer"},
	}

	loop := &agentRecordingLoop{}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform:     &scriptedPlatform{events: []agentkit.MessageEvent{userEvent(sessionID, "hi")}},
		Loop:         loop,
		SessionStore: store,
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

func TestRunnerMessageAgentOverridesSessionBind(t *testing.T) {
	t.Parallel()

	const sessionID = agentkit.SessionID("cli:test")
	mem, err := session.NewMemory(session.MemoryConfig{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	store := mapSessionStore{
		sessions: map[agentkit.SessionID]agentkit.Session{sessionID: mem},
		binds:    map[agentkit.SessionID]agentkit.AgentID{sessionID: "reviewer"},
	}

	loop := &agentRecordingLoop{}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform: &scriptedPlatform{events: []agentkit.MessageEvent{{
			SessionID: sessionID,
			AgentID:   "assistant",
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "override"}},
			},
		}}},
		Loop:         loop,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if loop.lastAgent != "assistant" {
		t.Fatalf("agent = %q, want assistant", loop.lastAgent)
	}
}
