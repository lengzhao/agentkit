package agentkit

import "context"

type Policy interface {
	Evaluate(context.Context, PolicyInput) (Decision, error)
}

// PolicyInput carries the policy payload. Per-conversation policy should read
// ctx.Value(KeySessionID) / ctx.Value(KeyAgentID).
type PolicyInput struct {
	ToolCall *ToolCall
}

type DecisionKind string

const (
	DecisionAllow DecisionKind = "allow"
	DecisionDeny  DecisionKind = "deny"
	DecisionAsk   DecisionKind = "ask"
)

type Decision struct {
	Kind   DecisionKind
	Reason string
	Audit  map[string]string
}

type Approval interface {
	Ask(context.Context, ApprovalRequest) (ApprovalDecision, error)
}

type ApprovalRequest struct {
	Reason   string
	ToolCall *ToolCall
}

type ApprovalDecision struct {
	Allowed bool
	Reason  string
	Audit   map[string]string
}
