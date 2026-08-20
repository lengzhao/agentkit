package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
)

type RuntimeConfig struct {
	DefaultTimeoutSeconds int `json:"defaultTimeoutSeconds"`
}

type RuntimeDeps struct {
	Tools    []agentkit.Tool   `json:"tools"`
	Policies []agentkit.Policy `json:"policies,omitempty"`
	Approval agentkit.Approval `json:"approval,omitempty"`
}

// Runtime executes tools through the policy and approval pipeline.
type Runtime struct {
	tools    map[string]agentkit.Tool
	policies []agentkit.Policy
	approval agentkit.Approval
}

func NewRuntime(_ RuntimeConfig, deps RuntimeDeps) (*Runtime, error) {
	tools := make(map[string]agentkit.Tool, len(deps.Tools))
	for _, tool := range deps.Tools {
		if tool == nil {
			continue
		}
		name := tool.Name()
		if _, ok := tools[name]; ok {
			return nil, fmt.Errorf("duplicate tool name %q", name)
		}
		tools[name] = tool
	}
	return &Runtime{
		tools:    tools,
		policies: deps.Policies,
		approval: deps.Approval,
	}, nil
}

func (r *Runtime) Visible(_ context.Context, _ agentkit.ToolScope) ([]agentkit.ToolSpec, error) {
	specs := make([]agentkit.ToolSpec, 0, len(r.tools))
	for _, tool := range r.tools {
		specs = append(specs, agentkit.ToolSpec{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		})
	}
	return specs, nil
}

func (r *Runtime) Execute(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	scope := scopeFrom(ctx)
	tool, ok := r.tools[call.Name]
	if !ok {
		return deniedResult(call, "tool not found"), nil
	}

	decision, err := r.evaluatePolicies(ctx, scope, call)
	if err != nil {
		return agentkit.ToolResult{}, err
	}
	switch decision.Kind {
	case agentkit.DecisionDeny:
		return deniedResult(call, decision.Reason), nil
	case agentkit.DecisionAsk:
		if r.approval == nil {
			return deniedResult(call, "approval required but no approval provider configured"), nil
		}
		approval, err := r.approval.Ask(ctx, agentkit.ApprovalRequest{
			SessionID: scope.SessionID,
			AgentID:   scope.AgentID,
			Reason:    decision.Reason,
			Policy:    decision,
			ToolCall:  &call,
		})
		if err != nil {
			return agentkit.ToolResult{}, err
		}
		if !approval.Allowed {
			reason := approval.Reason
			if reason == "" {
				reason = "approval denied"
			}
			return deniedResult(call, reason), nil
		}
	}

	slog.Info("tool execute", "tool", call.Name, "session_id", scope.SessionID, "agent_id", scope.AgentID)
	return tool.Call(ctx, call)
}

func (r *Runtime) evaluatePolicies(ctx context.Context, scope agentkit.ToolScope, call agentkit.ToolCall) (agentkit.Decision, error) {
	input := agentkit.PolicyInput{
		SessionID: scope.SessionID,
		AgentID:   scope.AgentID,
		ToolCall:  &call,
		Action:    "tool/call",
		Resource:  call.Name,
	}
	for _, policy := range r.policies {
		if policy == nil {
			continue
		}
		decision, err := policy.Evaluate(ctx, input)
		if err != nil {
			return agentkit.Decision{}, err
		}
		switch decision.Kind {
		case agentkit.DecisionDeny, agentkit.DecisionAsk:
			return decision, nil
		}
	}
	return agentkit.Allow(), nil
}

func deniedResult(call agentkit.ToolCall, reason string) agentkit.ToolResult {
	if reason == "" {
		reason = "denied"
	}
	return agentkit.ToolResult{
		ID:   call.ID,
		Name: call.Name,
		Content: []agentkit.ToolContent{{
			Type: "text",
			Text: reason,
		}},
		Audit: map[string]string{"decision": "deny", "reason": reason},
	}
}

func ResultText(result agentkit.ToolResult) string {
	var b strings.Builder
	for _, part := range result.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}
