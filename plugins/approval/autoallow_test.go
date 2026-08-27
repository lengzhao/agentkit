package approval_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/approval"
)

func TestAutoAllowApproves(t *testing.T) {
	t.Parallel()

	svc, err := approval.NewAutoAllow(approval.AutoAllowConfig{})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := svc.Ask(context.Background(), agentkit.ApprovalRequest{
		Reason:   "shell command is not in the allowlist",
		ToolCall: &agentkit.ToolCall{ID: "call", Name: "bash"},
	})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if !decision.Allowed {
		t.Fatal("auto-allow must approve")
	}
	if decision.Reason == "" {
		t.Fatal("expected a configured reason on approval")
	}
}

func TestAutoAllowUsesConfiguredReason(t *testing.T) {
	t.Parallel()

	svc, err := approval.NewAutoAllow(approval.AutoAllowConfig{Reason: "nightly job"})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := svc.Ask(context.Background(), agentkit.ApprovalRequest{})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if decision.Reason != "nightly job" {
		t.Fatalf("reason = %q, want %q", decision.Reason, "nightly job")
	}
}
