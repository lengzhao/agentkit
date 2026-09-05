package subagent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/session"
)

type storeRecordingAgent struct {
	id      agentkit.AgentID
	summary string
	store   agentkit.SessionStore
}

func (a *storeRecordingAgent) ID() agentkit.AgentID { return a.id }

func (a *storeRecordingAgent) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	sessionID := session.SessionIDFromContext(ctx)
	sess, err := a.store.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	return session.AppendMessage(ctx, sess, a.id, agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: a.summary}},
	})
}

func newLoopSpawner(t *testing.T, async bool, summary string) (*LoopAgentSpawner, agentkit.SessionStore, chan agentkit.MessageEvent) {
	t.Helper()
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{
		Workspace: ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := &storeRecordingAgent{id: "cursor", summary: summary, store: store}
	spawner, err := NewLoopAgent(LoopAgentConfig{
		Agents: []LoopAgentEntry{{
			Name:        "cursor",
			Description: "coding helper",
			Agent:       "cursor",
			Async:       async,
		}},
	}, LoopAgentDeps{
		SessionStore: store,
		Agents:       []agentkit.Agent{agent},
	})
	if err != nil {
		t.Fatal(err)
	}
	loop := spawner.(*LoopAgentSpawner)
	ch := make(chan agentkit.MessageEvent, 1)
	var once sync.Once
	loop.BindSubmit(func(_ context.Context, event agentkit.MessageEvent) error {
		once.Do(func() { ch <- event })
		return nil
	})
	return loop, store, ch
}

func loopParentCtx() context.Context {
	env := agentkit.TurnEnvelope{
		Route:        agentkit.SessionRoute("cli", "cli:default:t:1:u:user-1"),
		Conversation: "cli:default",
		Workspace:    "cli:default",
		Actor:        agentkit.ActorRef{UserID: "user-1"},
	}
	ctx := session.ApplyEnvelopeToContext(context.Background(), env)
	ctx = session.WithAgentID(ctx, agentkit.AgentID("assistant"))
	return ctx
}

func TestLoopAgentInheritsParentEnvelope(t *testing.T) {
	t.Parallel()

	var captured agentkit.TurnEnvelope
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	agent := &envelopeCapturingAgent{id: "cursor", store: store, capture: &captured}
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
	result, err := spawner.Run(loopParentCtx(), capsubagent.Request{Agent: "cursor", Task: "check envelope"})
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := session.RouteSessionID(captured.Route); !ok || id != "cli:default:t:1:u:user-1" {
		t.Fatalf("route = %v", captured.Route)
	}
	if captured.Workspace != "cli:default" {
		t.Fatalf("workspace = %q", captured.Workspace)
	}
	if captured.Conversation != result.Session {
		t.Fatalf("conversation = %q, child session = %q", captured.Conversation, result.Session)
	}
	if captured.Conversation == "cli:default" {
		t.Fatal("child should use derived conversation id")
	}
}

type envelopeCapturingAgent struct {
	id      agentkit.AgentID
	store   agentkit.SessionStore
	capture *agentkit.TurnEnvelope
}

func (a *envelopeCapturingAgent) ID() agentkit.AgentID { return a.id }

func (a *envelopeCapturingAgent) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	*a.capture = session.EnvelopeFromContext(ctx)
	sessionID := session.SessionIDFromContext(ctx)
	sess, err := a.store.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	return session.AppendMessage(ctx, sess, a.id, agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "ok"}},
	})
}

func TestLoopAgentSyncReturnsSummary(t *testing.T) {
	t.Parallel()

	spawner, _, _ := newLoopSpawner(t, false, "refactor complete")
	result, err := spawner.Run(loopParentCtx(), capsubagent.Request{Agent: "cursor", Task: "refactor auth"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != capsubagent.StatusStopped {
		t.Fatalf("status = %q", result.Status)
	}
	if result.Summary != "refactor complete" {
		t.Fatalf("summary = %q", result.Summary)
	}
}

func TestLoopAgentAsyncSubmitsFollowUp(t *testing.T) {
	t.Parallel()

	spawner, _, ch := newLoopSpawner(t, true, "async done")
	result, err := spawner.Run(loopParentCtx(), capsubagent.Request{Agent: "cursor", Task: "long job"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != capsubagent.StatusRunning {
		t.Fatalf("status = %q, want running", result.Status)
	}
	select {
	case event := <-ch:
		if event.AgentID != "assistant" {
			t.Fatalf("agent id = %q", event.AgentID)
		}
		if len(event.Message.Content) == 0 || event.Message.Content[0].Text == "" {
			t.Fatal("empty follow-up text")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected async follow-up submit")
	}
}

func TestLoopAgentRejectsSecondAsync(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	unblock := make(chan struct{})
	finished := make(chan struct{})
	root := t.TempDir()
	ws := rtworkspace.Static(root)
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	agent := &blockingLoopAgent{id: "cursor", started: started, unblock: unblock, finished: finished, store: store}
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
	if _, err := loop.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "first job"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first async job did not start")
	}
	if _, err := loop.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "second job"}); err == nil {
		t.Fatal("expected second async job to be rejected")
	}
	close(unblock)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("first async job did not finish")
	}
}

type blockingLoopAgent struct {
	id      agentkit.AgentID
	started chan struct{}
	unblock chan struct{}
	finished chan struct{}
	store   agentkit.SessionStore
}

func (a *blockingLoopAgent) ID() agentkit.AgentID { return a.id }

func (a *blockingLoopAgent) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	close(a.started)
	<-a.unblock
	sessionID := session.SessionIDFromContext(ctx)
	sess, err := a.store.Get(ctx, sessionID)
	if err != nil {
		if a.finished != nil {
			close(a.finished)
		}
		return err
	}
	err = session.AppendMessage(ctx, sess, a.id, agentkit.EventAssistantMessage, agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "done"}},
	})
	if a.finished != nil {
		close(a.finished)
	}
	return err
}

func TestLoopAgentDefinitionsFromConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := session.NewStore(session.StoreConfig{Dir: "."}, session.StoreDeps{
		Workspace: rtworkspace.Static(root),
	})
	if err != nil {
		t.Fatal(err)
	}
	spawner, err := NewLoopAgent(LoopAgentConfig{
		Agents: []LoopAgentEntry{{
			Name:        "cursor",
			Description: "loop-backed coder",
			Agent:       "cursor",
			Async:       true,
		}},
	}, LoopAgentDeps{
		SessionStore: store,
		Agents:       []agentkit.Agent{&storeRecordingAgent{id: "cursor", store: store}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defs, err := spawner.Definitions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].LoopAgent != "cursor" || !defs[0].Async {
		t.Fatalf("defs = %+v", defs)
	}
}

func TestCompositeRoutesToLoop(t *testing.T) {
	t.Parallel()

	in := &routeFakeSpawner{defs: []capsubagent.Definition{{Name: "researcher", Description: "read"}}}
	loop := &routeFakeSpawner{defs: []capsubagent.Definition{{Name: "cursor", Description: "code", Backend: capsubagent.BackendLoop, Async: true}}}
	loop.result = capsubagent.Result{Agent: "cursor", Status: capsubagent.StatusRunning, JobID: "job-1"}
	composite, err := NewComposite(struct{}{}, CompositeDeps{Inprocess: in, Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	defs, err := composite.Definitions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 {
		t.Fatalf("defs = %d, want 2", len(defs))
	}
	result, err := composite.Run(context.Background(), capsubagent.Request{Agent: "cursor", Task: "do work"})
	if err != nil {
		t.Fatal(err)
	}
	if result.JobID != "job-1" {
		t.Fatalf("result = %+v", result)
	}
	if loop.last.Agent != "cursor" {
		t.Fatalf("loop request = %+v", loop.last)
	}
}

type routeFakeSpawner struct {
	defs   []capsubagent.Definition
	result capsubagent.Result
	err    error
	last   capsubagent.Request
}

func (f *routeFakeSpawner) Definitions(context.Context) ([]capsubagent.Definition, error) {
	return f.defs, nil
}

func (f *routeFakeSpawner) Run(_ context.Context, req capsubagent.Request) (capsubagent.Result, error) {
	f.last = req
	return f.result, f.err
}
