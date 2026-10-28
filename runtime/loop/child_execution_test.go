package loop

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/execution"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func TestChildExecutionPreservesParentBrokerOutcomes(t *testing.T) {
	for _, decision := range []string{"yes", "no", permission.DecisionAllowAll, "timeout", "cancel"} {
		t.Run(decision, func(t *testing.T) {
			ctrl := NewControl()
			defer ctrl.Stop()
			var requestID string
			requests := 0
			emit := func(_ context.Context, event agentkit.OutboundEvent) error {
				if event.Type != agentkit.EventPermissionRequest {
					return nil
				}
				requests++
				var p permission.RequestPayload
				if err := json.Unmarshal(event.Data, &p); err != nil {
					return err
				}
				requestID = p.ID
				if p.Conversation != "parent" || event.AgentID != "reviewer" {
					t.Errorf("wrong permission scope: %+v %+v", p, event)
				}
				switch decision {
				case "timeout":
				case "cancel":
					_ = ctrl.Cancel(context.Background(), "user stop")
				default:
					if !ctrl.DeliverPermissionReply("parent", permission.Reply{RequestID: p.ID, Text: decision, UserID: "U1"}) {
						t.Error("reply rejected")
					}
				}
				return nil
			}
			parent := turnContext(context.Background(), "parent", "delivery", "assistant", "cli", "U1", ctrl, emit, interactiveCapability())
			parent = rctx.WithWorkspace(parent, "tenant")
			child, close, err := execution.NewChild(parent, execution.ChildOptions{HistorySessionID: "child", AgentID: "reviewer"})
			if err != nil {
				t.Fatal(err)
			}
			defer close()
			broker, _ := permission.BrokerFrom(child)
			req := permission.Request{ID: "backend-id", Kind: permission.KindAllowDeny, ToolCall: &agentkit.ToolCall{Name: "shell"}, Timeout: 20 * time.Millisecond}
			result, err := broker.Await(child, req)
			if err != nil {
				t.Fatal(err)
			}
			want := permission.OutcomeResolved
			if decision == "timeout" {
				want = permission.OutcomeTimeout
			}
			if decision == "cancel" {
				want = permission.OutcomeCancelled
			}
			if result.Outcome != want || result.Allow != (decision == "yes" || decision == permission.DecisionAllowAll) {
				t.Fatalf("result=%+v", result)
			}
			if ctrl.DeliverPermissionReply("parent", permission.Reply{RequestID: requestID, Text: "yes"}) {
				t.Fatal("late reply accepted after completion")
			}
			if decision == permission.DecisionAllowAll {
				result, err = broker.Await(child, req)
				if err != nil || !result.Allow || requests != 1 {
					t.Fatalf("parent allow-all lost: result=%+v err=%v requests=%d", result, err, requests)
				}
			}
		})
	}
}

func TestChildExecutionCannotApproveAfterCleanup(t *testing.T) {
	ctrl := NewControl()
	defer ctrl.Stop()
	ctrl.setPermissionAllowAll()
	parent := turnContext(context.Background(), "parent", "delivery", "assistant", "cli", "U1", ctrl, nil, interactiveCapability())
	parent = rctx.WithWorkspace(parent, "tenant")
	child, close, err := execution.NewChild(parent, execution.ChildOptions{HistorySessionID: "child", AgentID: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	broker, _ := permission.BrokerFrom(child)
	close()
	result, err := broker.Await(child, permission.Request{Kind: permission.KindAllowDeny, ToolCall: &agentkit.ToolCall{Name: "shell"}})
	if err != context.Canceled || result.Allow {
		t.Fatalf("closed scope authorized request: result=%+v err=%v", result, err)
	}
}

func TestChildApprovalDeadlineSurvivesIndependentCallbackContext(t *testing.T) {
	ctrl := NewControl()
	defer ctrl.Stop()
	parent := turnContext(context.Background(), "parent", "delivery", "assistant", "cli", "U1", ctrl,
		func(context.Context, agentkit.OutboundEvent) error { return nil }, interactiveCapability())
	parent = rctx.WithWorkspace(parent, "tenant")
	parent, cancel := context.WithTimeout(parent, 30*time.Millisecond)
	defer cancel()
	child, close, err := execution.NewChild(parent, execution.ChildOptions{HistorySessionID: "child", AgentID: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	broker, _ := permission.BrokerFrom(child)
	result, err := broker.Await(context.WithoutCancel(child), permission.Request{Kind: permission.KindAllowDeny, ToolCall: &agentkit.ToolCall{Name: "shell"}, Timeout: time.Second})
	if err != nil || result.Outcome != permission.OutcomeTimeout {
		t.Fatalf("deadline classified as cancellation: result=%+v err=%v", result, err)
	}
}
