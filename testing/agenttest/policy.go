package agenttest

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// denyToolPolicy denies every tool call with a fixed reason.
type denyToolPolicy struct {
	reason string
}

func (p denyToolPolicy) Evaluate(_ context.Context, _ agentkit.PolicyInput) (agentkit.Decision, error) {
	return agentkit.Decision{Kind: agentkit.DecisionDeny, Reason: p.reason}, nil
}

// DenyAllToolsPolicy returns a policy that blocks all tool execution.
func DenyAllToolsPolicy(reason string) agentkit.Policy {
	if reason == "" {
		reason = "denied by smoke policy"
	}
	return denyToolPolicy{reason: reason}
}
