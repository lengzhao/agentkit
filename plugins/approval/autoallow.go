package approval

import (
	"context"
	"log/slog"

	"github.com/lengzhao/agentkit"
)

// AutoAllowConfig configures unattended approval.
type AutoAllowConfig struct {
	// Reason is recorded on every decision, e.g. "unattended run".
	Reason string `json:"reason"`
}

// NewAutoAllow approves every ask so an unattended run never blocks on a human.
// It performs no filtering: the only real enforcement is the Policy plane, so
// pair this with policy/shell-allowlist and policy/path-denylist. Every decision
// is logged and audited.
func NewAutoAllow(cfg AutoAllowConfig) (agentkit.Approval, error) {
	reason := cfg.Reason
	if reason == "" {
		reason = "auto-allowed: unattended run"
	}
	return autoAllow{reason: reason}, nil
}

type autoAllow struct {
	reason string
}

func (a autoAllow) Ask(ctx context.Context, req agentkit.ApprovalRequest) (agentkit.ApprovalDecision, error) {
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	toolName := ""
	if req.ToolCall != nil {
		toolName = req.ToolCall.Name
	}
	slog.Warn("approval auto-allowed",
		"session_id", sessionID,
		"tool", toolName,
		"policy_reason", req.Reason,
	)
	return agentkit.ApprovalDecision{
		Allowed: true,
		Reason:  a.reason,
		Audit: map[string]string{
			"decision":      "auto-allow",
			"policy_reason": req.Reason,
		},
	}, nil
}
