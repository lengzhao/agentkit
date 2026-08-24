package approval_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/approval"
)

func TestAutoAllowApprovesAndAudits(t *testing.T) {
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
	// The audit trail is the only record an unattended run leaves behind.
	if decision.Audit["decision"] != "auto-allow" {
		t.Fatalf("audit decision = %q", decision.Audit["decision"])
	}
	if decision.Audit["policy_reason"] != "shell command is not in the allowlist" {
		t.Fatalf("audit policy_reason = %q", decision.Audit["policy_reason"])
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
