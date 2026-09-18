package runner_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/runner"
	"github.com/lengzhao/agentkit/runtime/session"
)

type stubPlatform struct{}

func (stubPlatform) Receive(context.Context) (agentkit.MessageEvent, error) {
	return agentkit.MessageEvent{}, io.EOF
}
func (stubPlatform) Send(context.Context, agentkit.OutboundEvent) error { return nil }

type stubStopLoop struct {
	busy      map[agentkit.SessionID]bool
	cancel    []cancelCall
	cancelErr error
}

type cancelCall struct {
	sessionID agentkit.SessionID
	reason    string
}

func (l *stubStopLoop) Dispatch(context.Context, agentkit.LoopRequest) error  { return nil }
func (l *stubStopLoop) Steer(context.Context, agentkit.ModelMessage) error    { return nil }
func (l *stubStopLoop) FollowUp(context.Context, agentkit.ModelMessage) error { return nil }
func (l *stubStopLoop) Cancel(ctx context.Context, reason string) error {
	sessionID := rctx.SessionIDFromContext(ctx)
	l.cancel = append(l.cancel, cancelCall{sessionID: sessionID, reason: reason})
	return l.cancelErr
}
func (l *stubStopLoop) CancelAllInFlight(string)                         {}
func (l *stubStopLoop) IsSessionBusy(id agentkit.SessionID) bool         { return l.busy[id] }
func (l *stubStopLoop) TryDeliverPermission(agentkit.MessageEvent) bool  { return false }
func (l *stubStopLoop) SupersedePendingForInbound(agentkit.MessageEvent) {}

func TestStopCommandNoTurnInProgress(t *testing.T) {
	t.Parallel()
	loop := &stubStopLoop{busy: map[agentkit.SessionID]bool{}}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform: stubPlatform{},
		Loop:     loop,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := root.(agentkit.CommandProvider)
	var stopCmd agentkit.Command
	for _, cmd := range provider.Commands() {
		if cmd.Name() == "stop" {
			stopCmd = cmd
			break
		}
	}
	if stopCmd == nil {
		t.Fatal("missing /stop command")
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:default", Workspace: "cli:default"})
	out, err := stopCmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "no turn in progress" {
		t.Fatalf("out = %q, want no turn in progress", out)
	}
	if len(loop.cancel) != 0 {
		t.Fatalf("cancel calls = %d, want 0", len(loop.cancel))
	}
}

func TestStopCommandCancelsBusySession(t *testing.T) {
	t.Parallel()
	sessionID := agentkit.SessionID("cli:default")
	loop := &stubStopLoop{busy: map[agentkit.SessionID]bool{sessionID: true}}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform: stubPlatform{},
		Loop:     loop,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := root.(agentkit.CommandProvider)
	var stopCmd agentkit.Command
	for _, cmd := range provider.Commands() {
		if cmd.Name() == "stop" {
			stopCmd = cmd
			break
		}
	}
	if stopCmd == nil {
		t.Fatal("missing /stop command")
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: string(sessionID), Workspace: string(sessionID)})
	out, err := stopCmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "stopping current turn" {
		t.Fatalf("out = %q", out)
	}
	if len(loop.cancel) != 1 {
		t.Fatalf("cancel calls = %d, want 1", len(loop.cancel))
	}
	if loop.cancel[0].sessionID != sessionID {
		t.Fatalf("cancel session = %q, want %q", loop.cancel[0].sessionID, sessionID)
	}
	if loop.cancel[0].reason != "/stop" {
		t.Fatalf("cancel reason = %q", loop.cancel[0].reason)
	}
}

func TestStopCommandCancelsBusyActiveChildSession(t *testing.T) {
	t.Parallel()
	delivery := session.BuildDeliverySessionID("lark", "oc_test", "", "ou_user")
	entry := session.ApplyScope(delivery, session.ScopeChannel, "ou_user")
	child := agentkit.SessionID(string(entry) + ":new:20260913")
	loop := &stubStopLoop{busy: map[agentkit.SessionID]bool{child: true}}
	store := stopActiveStore{active: map[agentkit.SessionID]agentkit.SessionID{entry: child}}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform:     stubPlatform{},
		Loop:         loop,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	var stopCmd agentkit.Command
	for _, cmd := range root.(agentkit.CommandProvider).Commands() {
		if cmd.Name() == "stop" {
			stopCmd = cmd
			break
		}
	}
	if stopCmd == nil {
		t.Fatal("missing /stop command")
	}
	env := session.ResolveEnvelope(agentkit.MessageEvent{
		PlatformID: "lark",
		UserID:     "ou_user",
	}, session.RoutePolicyForPlatform("lark", session.DefaultRoutePolicy(session.ScopeChannel)))
	env.Route = session.SessionRoute("lark", string(delivery))
	env = rctx.WithMetadataScope(env, session.ScopeChannel)
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), env)

	out, err := stopCmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "stopping current turn" {
		t.Fatalf("out = %q", out)
	}
	if len(loop.cancel) != 1 || loop.cancel[0].sessionID != child {
		t.Fatalf("cancel = %+v, want child %q", loop.cancel, child)
	}
}

type stopActiveStore struct {
	active map[agentkit.SessionID]agentkit.SessionID
}

func (s stopActiveStore) Get(context.Context, agentkit.SessionID) (agentkit.Session, error) {
	return nil, nil
}

func (s stopActiveStore) ActiveSession(_ context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	if active, ok := s.active[id]; ok {
		return active, nil
	}
	return id, nil
}

func (s stopActiveStore) SetActiveSession(_ context.Context, id, active agentkit.SessionID) error {
	s.active[id] = active
	return nil
}

func TestStopCommandRejectsArgs(t *testing.T) {
	t.Parallel()
	loop := &stubStopLoop{busy: map[agentkit.SessionID]bool{}}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform: stubPlatform{},
		Loop:     loop,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := root.(agentkit.CommandProvider)
	var stopCmd agentkit.Command
	for _, cmd := range provider.Commands() {
		if cmd.Name() == "stop" {
			stopCmd = cmd
			break
		}
	}
	ctx := rctx.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{Conversation: "cli:default", Workspace: "cli:default"})
	_, err = stopCmd.CommandExec(ctx, "now")
	if err == nil || !strings.Contains(err.Error(), "usage: /stop") {
		t.Fatalf("err = %v, want usage error", err)
	}
}
