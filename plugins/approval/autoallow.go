package approval

import (
	"context"
	"log/slog"

	"github.com/lengzhao/agentkit"
)

// AutoAllowConfig configures unattended approval.
type AutoAllowConfig struct {
	// Reason is text recorded on each decision; defaults to "auto-allowed: unattended run".
	Reason string `json:"reason"`
}

// NewAutoAllow registers approval/auto-allow: Allow every ask decision, for unattended runs.
//
// Best practices:
//   - Never use alone. It filters nothing, so the Policy plane is the only enforcement left: pair it with policy/shell-allowlist and policy/path-denylist.
//   - Every decision is logged via slog, so an unattended run stays reviewable after the fact.
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
	}, nil
}
