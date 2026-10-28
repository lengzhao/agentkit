package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	capworkspace "github.com/lengzhao/agentkit/cap/workspace"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type childScopeProbe struct {
	probe func(context.Context) error
}

func TestLoopChildCustomAgentRetainsWorkingDirectory(t *testing.T) {
	s, _, _ := newLoopSpawner(t, false, "done")
	ctx := capworkspace.WithWorkingDirectory(loopParentCtx(), "/custom/work")
	s.agents["cursor"] = childScopeProbe{probe: func(child context.Context) error {
		if got := capworkspace.WorkingDirectory(child); got != "/custom/work" {
			return fmt.Errorf("custom agent cwd=%q", got)
		}
		return nil
	}}
	if _, err := s.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "inspect"}); err != nil {
		t.Fatal(err)
	}
}

func TestLoopChildLegacyScopeFallback(t *testing.T) {
	for _, routed := range []bool{false, true} {
		t.Run(fmt.Sprint(routed), func(t *testing.T) {
			s, _, _ := newLoopSpawner(t, false, "done")
			ctx := rctx.WithWorkspace(loopParentCtx(), "")
			if !routed {
				ctx = rctx.WithRoute(ctx, agentkit.RouteRef{})
			}
			if _, err := s.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "legacy scope"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLoopChildSupportsNestedExecution(t *testing.T) {
	s, _, _ := newLoopSpawner(t, false, "done")
	calls := 0
	s.agents["cursor"] = childScopeProbe{probe: func(child context.Context) error {
		calls++
		if calls == 1 {
			_, err := s.Run(child, capsubagent.Request{Agent: "cursor", Task: "second layer"})
			return err
		}
		return nil
	}}
	if _, err := s.Run(loopParentCtx(), capsubagent.Request{Agent: "cursor", Task: "first layer"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("nested executions=%d", calls)
	}
}

func TestNestedAsyncChildOutlivesSynchronousScope(t *testing.T) {
	s, _, completed := newLoopSpawner(t, false, "done")
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	parent := context.WithValue(loopParentCtx(), agentkit.KeySessionControl, ctrl)
	outerEnded := make(chan struct{})
	observed := make(chan error, 1)
	s.agents["cursor"] = invocationProbe{run: func(child context.Context, in agentkit.TurnInput) error {
		if in.Message.Content[0].Text == "background" {
			<-outerEnded
			if _, ok := permission.BrokerFrom(child); !ok {
				observed <- fmt.Errorf("background broker lost")
				return nil
			}
			control := child.Value(agentkit.KeySessionControl).(interface {
				BeginStep(context.Context) (context.Context, func())
			})
			step, end := control.BeginStep(child)
			defer end()
			observed <- step.Err()
			return nil
		}
		async := true
		_, err := s.Run(child, capsubagent.Request{Agent: "cursor", Task: "background", Async: &async})
		return err
	}}
	_, err := s.Run(parent, capsubagent.Request{Agent: "cursor", Task: "outer"})
	close(outerEnded)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-observed:
		if err != nil {
			t.Fatalf("synchronous cleanup cancelled background child: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("background child did not run")
	}
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("background follow-up did not complete")
	}
}

func (childScopeProbe) ID() agentkit.AgentID { return "cursor" }
func (p childScopeProbe) RunTurn(ctx context.Context, _ agentkit.TurnInput) error {
	return p.probe(ctx)
}

type interactiveParentControl struct{ *loop.Control }

func (interactiveParentControl) PermissionCapability() permission.Capability {
	return permission.Capability{Interactive: true}
}

func TestLoopChildApprovalReturnsToParentConversation(t *testing.T) {
	s, _, _ := newLoopSpawner(t, false, "done")
	ctrl := interactiveParentControl{loop.NewControl()}
	defer ctrl.Stop()
	ctx := context.WithValue(loopParentCtx(), agentkit.KeySessionControl, ctrl)
	var payload permission.RequestPayload
	ctx = rctx.ContextWithOutboundEmit(ctx, func(_ context.Context, ev agentkit.OutboundEvent) error {
		if ev.Type != agentkit.EventPermissionRequest {
			return nil
		}
		if err := json.Unmarshal(ev.Data, &payload); err != nil {
			return err
		}
		if ev.AgentID != "cursor" || rctx.OutboundRouteID(ev) != "cli:default:t:1:u:user-1" {
			return fmt.Errorf("wrong approval identity or route: %+v", ev)
		}
		if !ctrl.DeliverPermissionReply("cli:default", permission.Reply{RequestID: payload.ID, Text: "yes", UserID: "user-1"}) {
			return fmt.Errorf("parent did not accept reply")
		}
		return nil
	})
	s.agents["cursor"] = childScopeProbe{probe: func(child context.Context) error {
		// Ordinary child output may be filtered; approval must use the trusted
		// parent's interactive outbound hook rather than that filtered stream.
		child = rctx.ContextWithOutboundEmit(child, func(context.Context, agentkit.OutboundEvent) error {
			return fmt.Errorf("approval reached filtered child output")
		})
		broker, ok := permission.BrokerFrom(child)
		if !ok {
			return fmt.Errorf("no child broker")
		}
		result, err := broker.Await(child, permission.Request{ID: "remote-id", Kind: permission.KindAllowDeny, ToolCall: &agentkit.ToolCall{Name: "shell"}})
		if err != nil {
			return err
		}
		if !result.Allow {
			return fmt.Errorf("human authorization lost: %+v", result)
		}
		return nil
	}}
	_, err := s.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "approval"})
	if err != nil {
		t.Fatal(err)
	}
	if payload.Conversation != "cli:default" {
		t.Fatalf("approval answer would go to child history %q", payload.Conversation)
	}
	if payload.ID == "remote-id" {
		t.Fatal("backend request ID reused as host approval ID")
	}
}

func TestLoopChildCleanupPreservesParentCancellation(t *testing.T) {
	s, _, _ := newLoopSpawner(t, false, "done")
	ctrl := loop.NewControl()
	defer ctrl.Stop()
	ctx := context.WithValue(loopParentCtx(), agentkit.KeySessionControl, ctrl)
	ctx, parentEnd := ctrl.BeginStep(ctx)
	defer parentEnd()
	s.agents["cursor"] = childScopeProbe{probe: func(child context.Context) error {
		local, ok := child.Value(agentkit.KeySessionControl).(interface {
			BeginStep(context.Context) (context.Context, func())
		})
		if !ok {
			return fmt.Errorf("no child control")
		}
		_, end := local.BeginStep(child)
		end()
		return nil
	}}
	if _, err := s.Run(ctx, capsubagent.Request{Agent: "cursor", Task: "finish"}); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("finishing child cancelled parent")
	}
	_ = ctrl.Cancel(ctx, "stop parent after child")
	if ctx.Err() != context.Canceled {
		t.Fatal("child removed parent's stop registration")
	}
}
