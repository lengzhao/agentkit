package approvalautodeny

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("approval/auto-deny", New)
}

func New() (agentkit.Approval, error) {
	return autoDeny{}, nil
}

type autoDeny struct{}

func (autoDeny) Ask(_ context.Context, req agentkit.ApprovalRequest) (agentkit.ApprovalDecision, error) {
	return agentkit.ApprovalDecision{Allowed: false, Reason: req.Reason}, nil
}
