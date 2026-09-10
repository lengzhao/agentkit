package acpremote

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/lengzhao/agentkit/cap/permission"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
)

func permissionRequestWithReject() acp.RequestPermissionRequest {
	return acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "allow-once", Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow"},
			{OptionId: "reject-once", Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject"},
		},
	}
}

func TestMapPermissionResultToACPTimeoutUsesRejectWithMeta(t *testing.T) {
	result := rtpermission.TimedOut(permission.Request{Kind: permission.KindAllowDeny})
	resp := mapPermissionResultToACP(permissionRequestWithReject(), result)

	if resp.Outcome.Selected == nil || resp.Outcome.Selected.OptionId != "reject-once" {
		t.Fatalf("expected reject-once, got %+v", resp.Outcome)
	}
	if resp.Outcome.Cancelled != nil {
		t.Fatal("timeout should not cancel the turn")
	}
	if resp.Meta["outcome"] != string(permission.OutcomeTimeout) {
		t.Fatalf("meta outcome = %v", resp.Meta["outcome"])
	}
	if resp.Meta["guidance"] == "" {
		t.Fatal("expected guidance in meta")
	}
}

func TestMapPermissionResultToACPTurnCancelUsesCancelled(t *testing.T) {
	result := rtpermission.Cancelled(permission.Request{Kind: permission.KindAllowDeny}, "permission abandoned: context canceled")
	resp := mapPermissionResultToACP(permissionRequestWithReject(), result)

	if resp.Outcome.Cancelled == nil {
		t.Fatalf("expected cancelled turn, got %+v", resp.Outcome)
	}
}

func TestMapPermissionResultToACPUserDenyUsesRejectWithMeta(t *testing.T) {
	result := permission.Result{
		Outcome: permission.OutcomeResolved,
		Allow:   false,
		Reason:  "user denied",
	}
	resp := mapPermissionResultToACP(permissionRequestWithReject(), result)

	if resp.Outcome.Selected == nil || resp.Outcome.Selected.OptionId != "reject-once" {
		t.Fatalf("expected reject-once, got %+v", resp.Outcome)
	}
	if resp.Meta["outcome"] != string(permission.OutcomeResolved) {
		t.Fatalf("meta outcome = %v", resp.Meta["outcome"])
	}
}
