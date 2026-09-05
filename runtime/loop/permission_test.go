package loop

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

func interactiveCapability() permission.Capability {
	return permission.Capability{
		Interactive:    true,
		DefaultTimeout: time.Minute,
		AnswerScope:    permission.ScopeAnyone,
	}
}

func turnContext(parent context.Context, sessionID, deliverySessionID agentkit.SessionID, agentID agentkit.AgentID, platformID, userID string, ctrl *Control, emit agentkit.OutboundEmit, cap permission.Capability) context.Context {
	if ctrl != nil {
		ctrl.setTurnCapability(cap)
	}
	routeID := deliverySessionID
	if routeID == "" {
		routeID = sessionID
	}
	env := agentkit.TurnEnvelope{
		Route:        agentkit.SessionRoute(platformID, string(routeID)),
		Conversation: string(sessionID),
		Actor:        agentkit.ActorRef{UserID: userID},
	}
	return withTurnContext(parent, env, sessionID, agentID, platformID, userID, nil, ctrl, emit)
}

type stubAgent struct{}

func (stubAgent) ID() agentkit.AgentID { return "coder" }
func (stubAgent) RunTurn(context.Context, agentkit.TurnInput) error {
	return nil
}

func TestAwaitQuestionResolvedAsync(t *testing.T) {
	t.Parallel()

	ctrl := NewControl()
	var started, resolved atomic.Int32
	emit := func(_ context.Context, out agentkit.OutboundEvent) error {
		switch out.Type {
		case agentkit.EventPermissionRequest:
			started.Add(1)
		case agentkit.EventPermissionResolved:
			resolved.Add(1)
		}
		return nil
	}
	ctx := turnContext(context.Background(), "feishu:oc_test", "feishu:oc_test", "coder", "feishu", "U1", ctrl, emit, interactiveCapability())

	done := make(chan permission.Result, 1)
	go func() {
		result, err := ctrl.Await(ctx, permission.Request{
			Kind: permission.KindQuestion,
			Question: &permission.Question{
				Prompt:  "pick one",
				Options: []permission.Option{{Label: "alpha"}, {Label: "beta"}},
			},
		})
		if err != nil {
			t.Error(err)
			return
		}
		done <- result
	}()

	deadline := time.After(2 * time.Second)
	for started.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("permission/request was not emitted")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	ctrl.mu.Lock()
	requestID := ctrl.permissionPending.requestID
	ctrl.mu.Unlock()
	if !ctrl.DeliverPermissionReply("feishu:oc_test", permission.Reply{
		RequestID: requestID,
		Text:      "2",
	}) {
		t.Fatal("expected deliver to succeed")
	}

	select {
	case result := <-done:
		if !result.Resolved() || result.Answer == nil || result.Answer.Text != "beta" {
			t.Fatalf("result = %+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("await did not finish")
	}
	if started.Load() != 1 || resolved.Load() != 1 {
		t.Fatalf("started=%d resolved=%d", started.Load(), resolved.Load())
	}
}

func TestAwaitNoHuman(t *testing.T) {
	t.Parallel()

	ctrl := NewControl()
	ctx := turnContext(context.Background(), "worker:job", "worker:job", "coder", "worker", "", ctrl, nil, permission.Capability{})

	result, err := ctrl.Await(ctx, permission.Request{
		Kind:     permission.KindQuestion,
		Question: &permission.Question{Prompt: "anyone there?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != permission.OutcomeNoHuman {
		t.Fatalf("result = %+v", result)
	}
}

func TestAwaitTimeout(t *testing.T) {
	t.Parallel()

	ctrl := NewControl()
	emit := func(context.Context, agentkit.OutboundEvent) error { return nil }
	ctx := turnContext(context.Background(), "feishu:oc_test", "feishu:oc_test", "coder", "feishu", "", ctrl, emit, permission.Capability{
		Interactive:    true,
		DefaultTimeout: 20 * time.Millisecond,
	})

	result, err := ctrl.Await(ctx, permission.Request{
		Kind:     permission.KindQuestion,
		Question: &permission.Question{Prompt: "waiting"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != permission.OutcomeTimeout {
		t.Fatalf("result = %+v", result)
	}
}

func TestDeliverPermissionReplyScopeAsker(t *testing.T) {
	t.Parallel()

	ctrl := NewControl()
	emit := func(context.Context, agentkit.OutboundEvent) error { return nil }
	ctx := turnContext(context.Background(), "slack:C1:U1", "slack:C1:U1", "coder", "slack", "U1", ctrl, emit, permission.Capability{
		Interactive: true,
		AnswerScope: permission.ScopeAsker,
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = ctrl.Await(ctx, permission.Request{
			ID:      "perm1",
			Kind:    permission.KindQuestion,
			AskedBy: "U1",
			Question: &permission.Question{
				Prompt: "confirm",
			},
		})
	}()

	waitForPending(t, ctrl)
	if ctrl.DeliverPermissionReply("slack:C1:U1", permission.Reply{
		RequestID: "perm1",
		UserID:    "U2",
		Text:      "yes",
	}) {
		t.Fatal("expected wrong user to be rejected")
	}
	if !ctrl.DeliverPermissionReply("slack:C1:U1", permission.Reply{
		RequestID: "perm1",
		UserID:    "U1",
		Text:      "yes",
	}) {
		t.Fatal("expected asker reply to be accepted")
	}
	wg.Wait()
}

func TestSupersedePending(t *testing.T) {
	t.Parallel()

	ctrl := NewControl()
	emit := func(context.Context, agentkit.OutboundEvent) error { return nil }
	ctx := turnContext(context.Background(), "feishu:oc_test", "feishu:oc_test", "coder", "feishu", "", ctrl, emit, interactiveCapability())

	done := make(chan permission.Result, 1)
	go func() {
		result, err := ctrl.Await(ctx, permission.Request{
			Kind:     permission.KindQuestion,
			Question: &permission.Question{Prompt: "pick"},
		})
		if err != nil {
			t.Error(err)
			return
		}
		done <- result
	}()

	waitForPending(t, ctrl)
	if !ctrl.SupersedePending("feishu:oc_test", "superseded by new inbound message") {
		t.Fatal("expected supersede to succeed")
	}

	select {
	case result := <-done:
		if result.Outcome != permission.OutcomeSuperseded {
			t.Fatalf("result = %+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("await did not finish after supersede")
	}
}

func TestAwaitReentrantPending(t *testing.T) {
	t.Parallel()

	ctrl := NewControl()
	emit := func(context.Context, agentkit.OutboundEvent) error { return nil }
	ctx := turnContext(context.Background(), "cli:default", "cli:default", "coder", "cli", "", ctrl, emit, permission.Capability{
		Interactive: true,
	})

	block := make(chan struct{})
	go func() {
		defer close(block)
		_, _ = ctrl.Await(ctx, permission.Request{
			ID:       "perm-block",
			Kind:     permission.KindQuestion,
			Question: &permission.Question{Prompt: "first"},
		})
	}()

	waitForPending(t, ctrl)

	_, err := ctrl.Await(ctx, permission.Request{
		Kind:     permission.KindQuestion,
		Question: &permission.Question{Prompt: "second"},
	})
	if err == nil {
		t.Fatal("expected reentrant await to fail")
	}
	ctrl.SupersedePending("cli:default", "test cleanup")
	<-block
}

func TestTryDeliverPermissionConsumesReply(t *testing.T) {
	t.Parallel()

	loop, err := New(Config{DefaultAgent: "coder"}, Deps{Agents: []agentkit.Agent{stubAgent{}}})
	if err != nil {
		t.Fatal(err)
	}
	l := loop.(*Default)
	ctrl := l.controlFor("feishu:oc_test")
	ctrl.registerPermissionPending(permission.Request{
		ID:       "perm1",
		Kind:     permission.KindQuestion,
		Question: &permission.Question{Prompt: "pick"},
	}, interactiveCapability())

	event := agentkit.MessageEvent{
		Envelope: agentkit.TurnEnvelope{Conversation: "feishu:oc_test"},
		Reply: permission.MarshalReply(permission.Reply{
			RequestID: "perm1",
			Text:      "beta",
		}),
	}
	if !l.TryDeliverPermission(event) {
		t.Fatal("expected deliver to succeed")
	}
	select {
	case reply := <-ctrl.permissionPending.replies:
		if reply.Text != "beta" {
			t.Fatalf("reply = %q", reply.Text)
		}
	default:
		t.Fatal("expected pending channel to receive reply")
	}
}

func waitForPending(t *testing.T, ctrl *Control) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		ctrl.mu.Lock()
		ok := ctrl.permissionPending != nil
		ctrl.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("pending was not registered")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}
