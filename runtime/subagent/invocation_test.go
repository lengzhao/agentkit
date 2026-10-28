package subagent

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/lengzhao/agentkit"
	capexecution "github.com/lengzhao/agentkit/cap/execution"
	"github.com/lengzhao/agentkit/cap/permission"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

type invocationProbe struct {
	run func(context.Context, agentkit.TurnInput) error
}

func (invocationProbe) ID() agentkit.AgentID { return "probe" }
func (a invocationProbe) RunTurn(ctx context.Context, in agentkit.TurnInput) error {
	return a.run(ctx, in)
}

func TestInvocationPreservesMessageAndOriginalError(t *testing.T) {
	_, store, _ := newLoopSpawner(t, false, "")
	parent := loopParentCtx()
	child := rctx.WithConversation(parent, "child")
	message := agentkit.ModelMessage{Role: "user", Content: []agentkit.ContentPart{
		{Type: "text", Text: "original task"}, {Type: "image", URL: "data:image/png;base64,AA", MIME: "image/png", Source: "work/image.png"},
	}}
	wantErr := errors.New("backend failed after partial answer")
	ag := invocationProbe{run: func(ctx context.Context, in agentkit.TurnInput) error {
		if !reflect.DeepEqual(in.Message, message) {
			t.Errorf("message changed: %+v", in.Message)
		}
		sess, err := store.Get(ctx, "child")
		if err != nil {
			return err
		}
		if err := sessevents.Default.AppendMessage(ctx, sess, "probe", agentkit.EventAssistantMessage, agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "partial answer"}}}); err != nil {
			return err
		}
		return wantErr
	}}
	out, err, close := invokeChild(parent, child, store, childInvocation{Name: "reviewer", Agent: ag, SessionID: "child", Message: message})
	if close != nil {
		close()
	}
	if !errors.Is(err, wantErr) || out.Summary != "partial answer" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestInvocationDoesNotReturnPriorTurnAnswer(t *testing.T) {
	_, store, _ := newLoopSpawner(t, false, "")
	parent := loopParentCtx()
	sess, err := store.Get(parent, "child")
	if err != nil {
		t.Fatal(err)
	}
	if err := sessevents.Default.AppendMessage(parent, sess, "probe", agentkit.EventAssistantMessage, agentkit.ModelMessage{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "old answer"}}}); err != nil {
		t.Fatal(err)
	}
	events, err := sess.Read(parent, 0)
	if err != nil {
		t.Fatal(err)
	}
	after := events[len(events)-1].Seq
	ag := invocationProbe{run: func(context.Context, agentkit.TurnInput) error { return nil }}
	out, err, close := invokeChild(parent, rctx.WithConversation(parent, "child"), store, childInvocation{Name: "reviewer", Agent: ag, SessionID: "child", After: after})
	if close != nil {
		close()
	}
	if err != nil || out.Summary != "" || out.Steps != 0 {
		t.Fatalf("old answer leaked: out=%+v err=%v", out, err)
	}
}

type failingHistoryStore struct{ agentkit.SessionStore }

func (failingHistoryStore) Get(context.Context, agentkit.SessionID) (agentkit.Session, error) {
	return nil, os.ErrNotExist
}

func TestInvocationPreservesSingleHistoryErrorIdentity(t *testing.T) {
	parent := loopParentCtx()
	ag := invocationProbe{run: func(context.Context, agentkit.TurnInput) error { return nil }}
	_, err, close := invokeChild(parent, rctx.WithConversation(parent, "child"), failingHistoryStore{}, childInvocation{Name: "reviewer", Agent: ag, SessionID: "child"})
	if close != nil {
		close()
	}
	if err != os.ErrNotExist || !os.IsNotExist(err) {
		t.Fatalf("legacy sentinel was wrapped: %v", err)
	}
}

type llmScopeProbe struct {
	agentkit.LLMProvider
	probe func(context.Context) error
}

func (p llmScopeProbe) Stream(ctx context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	if err := p.probe(ctx); err != nil {
		return nil, err
	}
	return p.LLMProvider.Stream(ctx, req)
}

func TestInprocessUsesSharedExecutionScope(t *testing.T) {
	f := newFixture(t, map[string]string{"researcher.md": researcherDef}, []llm.ScriptedStep{{Text: "done"}})
	s := f.spawner.(*Spawner)
	parentIdentity := capexecution.Identity{ID: "parent", RootTurnID: "root"}
	f.ctx = rctx.WithExecutionIdentity(f.ctx, parentIdentity)
	parentBroker := &brokerMarker{}
	f.ctx = context.WithValue(f.ctx, agentkit.KeySessionControl, parentBroker)
	called := false
	s.llm = llmScopeProbe{LLMProvider: s.llm, probe: func(ctx context.Context) error {
		called = true
		identity := rctx.ExecutionIdentityFromContext(ctx)
		if identity.ID == "" || identity.ID == parentIdentity.ID || identity.ParentID != "parent" || identity.RootTurnID != "root" {
			return errors.New("inprocess did not derive invocation identity")
		}
		broker, ok := permission.BrokerFrom(ctx)
		if !ok || broker == parentBroker {
			return errors.New("inprocess did not use isolated parent approval proxy")
		}
		return nil
	}}
	if _, err := s.Run(f.ctx, capsubagent.Request{Agent: "researcher", Task: "inspect"}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("model not called")
	}
}

func TestInprocessLegacyLocalContextWithoutWorkspace(t *testing.T) {
	f := newFixture(t, map[string]string{"researcher.md": researcherDef}, []llm.ScriptedStep{{Text: "legacy local result"}})
	f.ctx = rctx.WithWorkspace(f.ctx, "")
	result, err := f.spawner.Run(f.ctx, capsubagent.Request{Agent: "researcher", Task: "inspect"})
	if err != nil || result.Summary != "legacy local result" {
		t.Fatalf("legacy local caller regressed: result=%+v err=%v", result, err)
	}
}
