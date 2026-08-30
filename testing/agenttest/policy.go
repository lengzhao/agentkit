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

// AskAllToolsPolicy returns a policy that requires human approval for every tool call.
func AskAllToolsPolicy(reason string) agentkit.Policy {
	if reason == "" {
		reason = "approval required by smoke policy"
	}
	return askToolPolicy{reason: reason}
}

type askToolPolicy struct {
	reason string
}

func (p askToolPolicy) Evaluate(_ context.Context, _ agentkit.PolicyInput) (agentkit.Decision, error) {
	return agentkit.Decision{Kind: agentkit.DecisionAsk, Reason: p.reason}, nil
}

// DenyAllToolsPolicy returns a policy that blocks all tool execution.
func DenyAllToolsPolicy(reason string) agentkit.Policy {
	if reason == "" {
		reason = "denied by smoke policy"
	}
	return denyToolPolicy{reason: reason}
}
