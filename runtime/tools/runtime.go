package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
)

type RuntimeConfig struct {
	DefaultTimeoutSeconds int            `json:"defaultTimeoutSeconds"`
	MaxResultBytes        int            `json:"maxResultBytes"`
	ToolTimeouts          map[string]int `json:"toolTimeouts,omitempty"`
}

type RuntimeDeps struct {
	Tools    []agentkit.Tool      `json:"tools"`
	Policies []agentkit.Policy    `json:"policies,omitempty"`
	Approval agentkit.Approval    `json:"approval,omitempty"`
	Hooks    agentkit.HookRuntime `json:"hooks,omitempty"`
}

// Runtime executes tools through the policy and approval pipeline.
type Runtime struct {
	tools          map[string]agentkit.Tool
	policies       []agentkit.Policy
	approval       agentkit.Approval
	hooks          agentkit.HookRuntime
	defaultTimeout time.Duration
	maxResultBytes int
	toolTimeouts   map[string]time.Duration
}

func NewRuntime(cfg RuntimeConfig, deps RuntimeDeps) (*Runtime, error) {
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
	toolTimeouts := make(map[string]time.Duration, len(cfg.ToolTimeouts))
	for name, seconds := range cfg.ToolTimeouts {
		if seconds <= 0 {
			continue
		}
		toolTimeouts[name] = time.Duration(seconds) * time.Second
	}
	var defaultTimeout time.Duration
	if cfg.DefaultTimeoutSeconds > 0 {
		defaultTimeout = time.Duration(cfg.DefaultTimeoutSeconds) * time.Second
	}
	return &Runtime{
		tools:          tools,
		policies:       deps.Policies,
		approval:       deps.Approval,
		hooks:          deps.Hooks,
		defaultTimeout: defaultTimeout,
		maxResultBytes: cfg.MaxResultBytes,
		toolTimeouts:   toolTimeouts,
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

	if r.hooks != nil {
		if err := r.hooks.BeforeTool(ctx, &call); err != nil {
			return agentkit.ToolResult{}, err
		}
	}

	execCtx := ctx
	cancel := func() {}
	if timeout := r.timeoutFor(call.Name); timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	slog.Info("tool execute", "tool", call.Name, "session_id", scope.SessionID, "agent_id", scope.AgentID)
	result, err := tool.Call(execCtx, call)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return timeoutResult(call), nil
		}
		return agentkit.ToolResult{}, err
	}
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		return timeoutResult(call), nil
	}

	if r.hooks != nil {
		if err := r.hooks.AfterTool(ctx, &result); err != nil {
			return agentkit.ToolResult{}, err
		}
	}
	if r.maxResultBytes > 0 {
		result = compaction.TruncateToolResult(result, r.maxResultBytes)
	}
	return result, nil
}

func (r *Runtime) timeoutFor(name string) time.Duration {
	if timeout, ok := r.toolTimeouts[name]; ok {
		return timeout
	}
	return r.defaultTimeout
}

func (r *Runtime) evaluatePolicies(ctx context.Context, scope agentkit.ToolScope, call agentkit.ToolCall) (agentkit.Decision, error) {
	input := agentkit.PolicyInput{
		SessionID: scope.SessionID,
		AgentID:   scope.AgentID,
		ToolCall:  &call,
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
		Content: []agentkit.ContentPart{{
			Type: "text",
			Text: reason,
		}},
		Audit: map[string]string{"decision": "deny", "reason": reason},
	}
}

func timeoutResult(call agentkit.ToolCall) agentkit.ToolResult {
	return agentkit.ToolResult{
		ID:   call.ID,
		Name: call.Name,
		Content: []agentkit.ContentPart{{
			Type: "text",
			Text: "tool execution timed out",
		}},
		Audit: map[string]string{"decision": "timeout"},
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
