package agentkit

import "context"

type Policy interface {
	Evaluate(context.Context, PolicyInput) (Decision, error)
}

type PolicyInput struct {
	SessionID SessionID
	AgentID   AgentID
	ToolCall  *ToolCall
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
	SessionID SessionID
	AgentID   AgentID
	Reason    string
	Policy    Decision
	ToolCall  *ToolCall
}

type ApprovalDecision struct {
	Allowed bool
	Reason  string
	Audit   map[string]string
}
