package agenttest

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// AllowAll approves every tool execution.
type AllowAll struct{}

func (AllowAll) Ask(context.Context, agentkit.ApprovalRequest) (agentkit.ApprovalDecision, error) {
	return agentkit.ApprovalDecision{Allowed: true}, nil
}

// DenyAll rejects every tool execution.
type DenyAll struct{}

func (DenyAll) Ask(context.Context, agentkit.ApprovalRequest) (agentkit.ApprovalDecision, error) {
	return agentkit.ApprovalDecision{Allowed: false}, nil
}
