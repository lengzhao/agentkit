package approval

import (
	"context"

	"github.com/lengzhao/agentkit"
)

func NewAutoDeny() (agentkit.Approval, error) {
	return autoDeny{}, nil
}

type autoDeny struct{}

func (autoDeny) Ask(_ context.Context, req agentkit.ApprovalRequest) (agentkit.ApprovalDecision, error) {
	return agentkit.ApprovalDecision{Allowed: false, Reason: req.Reason}, nil
}
