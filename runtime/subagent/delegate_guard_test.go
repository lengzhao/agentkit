package subagent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
	"github.com/lengzhao/agentkit/runtime/session"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
)

// guardedStore blocks SessionStore.Get on sessions with an open turn, matching
// non-reentrant session/agent-guard wrappers used in production overlays.
type guardedStore struct {
	inner  agentkit.SessionStore
	active sync.Map
}

func newGuardedStore(inner agentkit.SessionStore) *guardedStore {
	return &guardedStore{inner: inner}
}

func (g *guardedStore) markTurnOpen(id agentkit.SessionID) { g.active.Store(id, struct{}{}) }

func (g *guardedStore) Get(ctx context.Context, id agentkit.SessionID) (agentkit.Session, error) {
	if _, ok := g.active.Load(id); ok {
		return nil, fmt.Errorf("guarded store: reentrant Get (%s)", id)
	}
	return g.inner.Get(ctx, id)
}

func TestLoopDelegateWithOpenSessionBypassesGuardedStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws := rtworkspace.Static(root)
	inner, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	guard := newGuardedStore(inner)

	agent := &storeRecordingAgent{id: "cursor", summary: "hi", store: guard}
	spawner, err := NewLoopAgent(LoopAgentConfig{
		Agents: []LoopAgentEntry{{
			Name:        "cursor",
			Description: "coding helper",
			Agent:       "cursor",
		}},
	}, LoopAgentDeps{
		SessionStore: guard,
		Agents:       []agentkit.Agent{agent},
	})
	if err != nil {
		t.Fatal(err)
	}

	const parentID = agentkit.SessionID("cli:default")
	ctx := loopParentCtx()
	open, err := inner.Get(ctx, parentID)
	if err != nil {
		t.Fatal(err)
	}
	guard.markTurnOpen(parentID)
	ctx = session.WithSession(ctx, open)

	result, err := spawner.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "say hi"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "hi" {
		t.Fatalf("summary = %q", result.Summary)
	}
}

func TestLoopDelegateWithoutOpenSessionFailsOnGuardedStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inner, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(root)})
	if err != nil {
		t.Fatal(err)
	}
	guard := newGuardedStore(inner)
	spawner, err := NewLoopAgent(LoopAgentConfig{
		Agents: []LoopAgentEntry{{
			Name:        "cursor",
			Description: "coding helper",
			Agent:       "cursor",
		}},
	}, LoopAgentDeps{
		SessionStore: guard,
		Agents:       []agentkit.Agent{&storeRecordingAgent{id: "cursor", summary: "hi", store: guard}},
	})
	if err != nil {
		t.Fatal(err)
	}

	guard.markTurnOpen(agentkit.SessionID("cli:default"))
	_, err = spawner.Run(loopParentCtx(), capsubagent.Request{Agent: "cursor", Task: "say hi"})
	if err == nil {
		t.Fatal("expected guarded store error without KeySession")
	}
}

func TestEmitSubagentLifecycleDoesNotBlockCaller(t *testing.T) {
	t.Parallel()

	parentSession := agentkit.SessionID("lark:delivery")
	ctx := session.ContextWithDeliveryRoute(context.Background(), "lark", parentSession)
	block := make(chan struct{})
	ctx = context.WithValue(ctx, agentkit.KeyOutboundEmit, agentkit.OutboundEmit(func(context.Context, agentkit.OutboundEvent) error {
		<-block
		return nil
	}))

	start := time.Now()
	emitSubagentLifecycle(ctx, "assistant", agentkit.EventSubagentStart, session.SubagentStartData{
		Agent: "cursor", Session: "sub:1", Task: "t",
	})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("emitSubagentLifecycle blocked %v", elapsed)
	}
	close(block)
}

func TestAsyncSubagentEndUsesRetainedParentSession(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inner, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(root)})
	if err != nil {
		t.Fatal(err)
	}
	guard := newGuardedStore(inner)
	const parentID = agentkit.SessionID("cli:default")
	var parentGets int32
	wrapped := &countingGuardStore{
		inner: guard,
		onGet: func(id agentkit.SessionID) {
			if id == parentID {
				atomic.AddInt32(&parentGets, 1)
			}
		},
	}

	agent := &storeRecordingAgent{id: "cursor", summary: "async hi", store: wrapped}
	spawner, err := NewLoopAgent(LoopAgentConfig{
		Agents: []LoopAgentEntry{{
			Name:        "cursor",
			Description: "coding helper",
			Agent:       "cursor",
			Async:       true,
		}},
	}, LoopAgentDeps{
		SessionStore: wrapped,
		Agents:       []agentkit.Agent{agent},
	})
	if err != nil {
		t.Fatal(err)
	}
	loop := spawner.(*LoopAgentSpawner)
	loop.BindSubmit(func(context.Context, agentkit.MessageEvent) error { return nil })

	ctx := loopParentCtx()
	open, err := inner.Get(ctx, parentID)
	if err != nil {
		t.Fatal(err)
	}
	guard.markTurnOpen(parentID)
	ctx = session.WithSession(ctx, open)
	ctx = context.WithValue(ctx, agentkit.KeyOutboundEmit, agentkit.OutboundEmit(func(context.Context, agentkit.OutboundEvent) error {
		return nil
	}))

	_, err = spawner.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "hi"})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		if atomic.LoadInt32(&parentGets) > 0 {
			break
		}
	}
	if n := atomic.LoadInt32(&parentGets); n != 0 {
		t.Fatalf("parent SessionStore.Get during async completion = %d, want 0 (use retained session)", n)
	}
}

type countingGuardStore struct {
	inner agentkit.SessionStore
	onGet func(agentkit.SessionID)
}

func (c *countingGuardStore) Get(ctx context.Context, id agentkit.SessionID) (agentkit.Session, error) {
	if c.onGet != nil {
		c.onGet(id)
	}
	return c.inner.Get(ctx, id)
}

func TestLoopDelegateAsyncReturnsBeforeSlowOutbound(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(root)})
	if err != nil {
		t.Fatal(err)
	}
	spawner, err := NewLoopAgent(LoopAgentConfig{
		Agents: []LoopAgentEntry{{
			Name:        "cursor",
			Description: "coding helper",
			Agent:       "cursor",
			Async:       true,
		}},
	}, LoopAgentDeps{
		SessionStore: store,
		Agents:       []agentkit.Agent{&storeRecordingAgent{id: "cursor", summary: "done", store: store}},
	})
	if err != nil {
		t.Fatal(err)
	}
	loop := spawner.(*LoopAgentSpawner)
	submitDone := make(chan struct{}, 1)
	loop.BindSubmit(func(context.Context, agentkit.MessageEvent) error {
		submitDone <- struct{}{}
		return nil
	})

	block := make(chan struct{})
	var unblockOnce sync.Once
	unblockOutbound := func() { unblockOnce.Do(func() { close(block) }) }
	t.Cleanup(unblockOutbound)
	ctx := loopParentCtx()
	open, err := store.Get(ctx, agentkit.SessionID("cli:default"))
	if err != nil {
		t.Fatal(err)
	}
	ctx = session.WithSession(ctx, open)
	ctx = context.WithValue(ctx, agentkit.KeyOutboundEmit, agentkit.OutboundEmit(func(context.Context, agentkit.OutboundEvent) error {
		<-block
		return nil
	}))

	start := time.Now()
	result, err := spawner.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "long"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != capsubagent.StatusRunning {
		t.Fatalf("status = %q", result.Status)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("async delegate blocked %v on outbound", elapsed)
	}
	unblockOutbound()
	select {
	case <-submitDone:
	case <-time.After(3 * time.Second):
		t.Fatal("async delegate did not finish after outbound unblocked")
	}
}

func TestLoopDelegateChildInheritsSessionControl(t *testing.T) {
	t.Parallel()

	var sawBroker bool
	ctrl := &brokerMarker{mark: &sawBroker}

	root := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(root)})
	if err != nil {
		t.Fatal(err)
	}
	agent := &brokerProbeAgent{id: "cursor", store: store, probe: &sawBroker}
	spawner, err := NewLoopAgent(LoopAgentConfig{
		Agents: []LoopAgentEntry{{
			Name:        "cursor",
			Description: "coding helper",
			Agent:       "cursor",
		}},
	}, LoopAgentDeps{
		SessionStore: store,
		Agents:       []agentkit.Agent{agent},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := loopParentCtx()
	ctx = context.WithValue(ctx, agentkit.KeySessionControl, ctrl)
	ctx = context.WithValue(ctx, agentkit.KeyOutboundEmit, agentkit.OutboundEmit(func(context.Context, agentkit.OutboundEvent) error {
		return nil
	}))
	open, err := store.Get(ctx, agentkit.SessionID("cli:default"))
	if err != nil {
		t.Fatal(err)
	}
	ctx = session.WithSession(ctx, open)

	_, err = spawner.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	if !sawBroker {
		t.Fatal("child turn did not inherit permission broker from parent control")
	}
}

func TestLoopDelegateAsyncChildInheritsSessionControl(t *testing.T) {
	t.Parallel()

	var sawBroker bool
	ctrl := &brokerMarker{mark: &sawBroker}

	root := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: rtworkspace.Static(root)})
	if err != nil {
		t.Fatal(err)
	}
	agent := &brokerProbeAgent{id: "cursor", store: store, probe: &sawBroker}
	spawner, err := NewLoopAgent(LoopAgentConfig{
		Agents: []LoopAgentEntry{{
			Name:        "cursor",
			Description: "coding helper",
			Agent:       "cursor",
			Async:       true,
		}},
	}, LoopAgentDeps{
		SessionStore: store,
		Agents:       []agentkit.Agent{agent},
	})
	if err != nil {
		t.Fatal(err)
	}
	loop := spawner.(*LoopAgentSpawner)
	loop.BindSubmit(func(context.Context, agentkit.MessageEvent) error { return nil })

	ctx := loopParentCtx()
	ctx = context.WithValue(ctx, agentkit.KeySessionControl, ctrl)
	ctx = context.WithValue(ctx, agentkit.KeyOutboundEmit, agentkit.OutboundEmit(func(context.Context, agentkit.OutboundEvent) error {
		return nil
	}))
	open, err := store.Get(ctx, agentkit.SessionID("cli:default"))
	if err != nil {
		t.Fatal(err)
	}
	ctx = session.WithSession(ctx, open)

	_, err = spawner.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "probe"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !sawBroker && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !sawBroker {
		t.Fatal("async child turn did not inherit permission broker from parent control")
	}
}

// brokerMarker is a minimal stand-in for *loop.Control in BrokerFrom tests.
type brokerMarker struct {
	mark *bool
}

func (b *brokerMarker) Await(context.Context, permission.Request) (permission.Result, error) {
	if b.mark != nil {
		*b.mark = true
	}
	return permission.Result{Outcome: permission.OutcomeResolved, Allow: true}, nil
}

type brokerProbeAgent struct {
	id    agentkit.AgentID
	store agentkit.SessionStore
	probe *bool
}

func (a *brokerProbeAgent) ID() agentkit.AgentID { return a.id }

func (a *brokerProbeAgent) RunTurn(ctx context.Context, input agentkit.TurnInput) error {
	if input.Emit == nil {
		return fmt.Errorf("emit required")
	}
	if _, ok := rtpermission.BrokerFrom(ctx); ok && a.probe != nil {
		*a.probe = true
	}
	sess, err := a.store.Get(ctx, session.SessionIDFromContext(ctx))
	if err != nil {
		return err
	}
	return session.AppendMessage(ctx, sess, a.id, agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "ok"}},
	})
}
