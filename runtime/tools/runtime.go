package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/cap/telemetry"
)

type RuntimeConfig struct {
	// DefaultTimeoutSeconds is per-call timeout when a tool has no specific entry.
	DefaultTimeoutSeconds int `json:"defaultTimeoutSeconds"`
	// MaxResultBytes is truncation limit applied to every tool result.
	MaxResultBytes int `json:"maxResultBytes"`
	// ToolTimeouts are per-tool timeout overrides, keyed by tool name.
	ToolTimeouts map[string]int `json:"toolTimeouts,omitempty"`
}

type RuntimeDeps struct {
	Tools        []agentkit.Tool         `json:"tools,omitempty"`
	ToolPacks    []agentkit.ToolPack     `json:"toolPacks,omitempty"`
	DynamicTools []agentkit.ToolProvider `json:"dynamicTools,omitempty"`
	Policies     []agentkit.Policy       `json:"policies,omitempty"`
	Approval     agentkit.Approval       `json:"approval,omitempty"`
	Hooks        agentkit.HookRuntime    `json:"hooks,omitempty"`
}

// Runtime executes tools through the policy and approval pipeline.
type Runtime struct {
	tools            map[string]agentkit.Tool
	dynamicProviders []agentkit.ToolProvider
	dynamicTools     map[string]agentkit.Tool
	dynamicMu        sync.Mutex
	policies         []agentkit.Policy
	approval         agentkit.Approval
	hooks            agentkit.HookRuntime
	defaultTimeout   time.Duration
	maxResultBytes   int
	toolTimeouts     map[string]time.Duration
}

// NewRuntime registers tools/runtime: Tool orchestration: visibility, policy evaluation, hooks, execution, result capping.
//
// Best practices:
//   - Policies are the enforcement plane; hooks only observe and rewrite. Put a security rule in a policy.
//   - The approval dep is consulted only for ask decisions, so it never sees an allowed or denied call.
func NewRuntime(cfg RuntimeConfig, deps RuntimeDeps) (agentkit.ToolRuntime, error) {
	tools := make(map[string]agentkit.Tool)
	for _, tool := range deps.Tools {
		if err := addTool(tools, tool); err != nil {
			return nil, err
		}
	}
	for _, pack := range deps.ToolPacks {
		for _, tool := range pack {
			if err := addTool(tools, tool); err != nil {
				return nil, err
			}
		}
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
		tools:            tools,
		dynamicProviders: deps.DynamicTools,
		dynamicTools:     make(map[string]agentkit.Tool),
		policies:         deps.Policies,
		approval:         deps.Approval,
		hooks:            deps.Hooks,
		defaultTimeout:   defaultTimeout,
		maxResultBytes:   cfg.MaxResultBytes,
		toolTimeouts:     toolTimeouts,
	}, nil
}

func addTool(tools map[string]agentkit.Tool, tool agentkit.Tool) error {
	if tool == nil {
		return nil
	}
	name := tool.Name()
	if _, ok := tools[name]; ok {
		return fmt.Errorf("duplicate tool name %q", name)
	}
	tools[name] = tool
	return nil
}

func (r *Runtime) refreshDynamic(ctx context.Context) error {
	if len(r.dynamicProviders) == 0 {
		r.dynamicMu.Lock()
		r.dynamicTools = make(map[string]agentkit.Tool)
		r.dynamicMu.Unlock()
		return nil
	}
	dynamic := make(map[string]agentkit.Tool)
	for _, provider := range r.dynamicProviders {
		if provider == nil {
			continue
		}
		tools, err := provider.ListTools(ctx)
		if err != nil {
			slog.Warn("dynamic tool provider failed", "error", err)
			continue
		}
		for _, tool := range tools {
			if tool == nil {
				continue
			}
			name := tool.Name()
			if _, ok := r.tools[name]; ok {
				return fmt.Errorf("duplicate tool name %q", name)
			}
			if _, ok := dynamic[name]; ok {
				return fmt.Errorf("duplicate tool name %q", name)
			}
			dynamic[name] = tool
		}
	}
	r.dynamicMu.Lock()
	r.dynamicTools = dynamic
	r.dynamicMu.Unlock()
	return nil
}

func (r *Runtime) Visible(ctx context.Context) ([]agentkit.ToolSpec, error) {
	if err := r.refreshDynamic(ctx); err != nil {
		return nil, err
	}
	specs := make([]agentkit.ToolSpec, 0, len(r.tools)+len(r.dynamicTools))
	for _, tool := range r.tools {
		specs = append(specs, agentkit.ToolSpec{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		})
	}
	r.dynamicMu.Lock()
	for _, tool := range r.dynamicTools {
		specs = append(specs, agentkit.ToolSpec{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		})
	}
	r.dynamicMu.Unlock()
	return specs, nil
}

func (r *Runtime) Execute(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	agentID, _ := ctx.Value(agentkit.KeyAgentID).(agentkit.AgentID)

	ctx = context.WithValue(ctx, agentkit.KeyToolCallID, call.ID)
	ctx, endObservation := telemetry.BeginObservation(ctx, telemetry.ObservationMetaFromContext(ctx, telemetry.ObservationMeta{
		Name:  "tool." + call.Name,
		Kind:  telemetry.KindTool,
		Input: string(call.Input),
	}))
	var observationEnd telemetry.ObservationEnd
	defer func() {
		endObservation(observationEnd)
	}()

	result, err := r.execute(ctx, call, sessionID, agentID)
	if err != nil {
		observationEnd.Err = err
		return result, err
	}
	observationEnd.Output = result.Content
	return result, nil
}

func (r *Runtime) execute(ctx context.Context, call agentkit.ToolCall, sessionID agentkit.SessionID, agentID agentkit.AgentID) (agentkit.ToolResult, error) {
	tool, ok := r.tools[call.Name]
	if !ok {
		if err := r.refreshDynamic(ctx); err != nil {
			return agentkit.ToolResult{}, err
		}
		r.dynamicMu.Lock()
		tool, ok = r.dynamicTools[call.Name]
		r.dynamicMu.Unlock()
		if !ok {
			return deniedResult(call, "tool not found", nil), nil
		}
	}

	decision, err := r.evaluatePolicies(ctx, call)
	if err != nil {
		return agentkit.ToolResult{}, err
	}
	switch decision.Kind {
	case agentkit.DecisionDeny:
		return deniedResult(call, decision.Reason, decision.Audit), nil
	case agentkit.DecisionAsk:
		allowed, reason, err := r.resolveAskDecision(ctx, &call, decision.Reason)
		if err != nil {
			return agentkit.ToolResult{}, err
		}
		if !allowed {
			if reason == "" {
				reason = "approval denied"
			}
			return deniedResult(call, reason, nil), nil
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

	slog.Info("tool execute", "tool", call.Name, "session_id", sessionID, "agent_id", agentID)
	output, err := tool.Call(execCtx, call.Input)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return timeoutResult(call), nil
		}
		return agentkit.ToolResult{}, err
	}
	if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
		return timeoutResult(call), nil
	}
	result := agentkit.ResultFromCall(call, output)

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

func (r *Runtime) resolveAskDecision(ctx context.Context, call *agentkit.ToolCall, policyReason string) (bool, string, error) {
	if r.approval != nil {
		decision, err := r.approval.Ask(ctx, agentkit.ApprovalRequest{
			Reason:   policyReason,
			ToolCall: call,
		})
		if err != nil {
			return false, "", err
		}
		return decision.Allowed, decision.Reason, nil
	}

	broker, ok := permission.BrokerFrom(ctx)
	if !ok {
		return false, "approval required but no permission broker on this session", nil
	}
	result, err := broker.Await(ctx, permission.Request{
		Kind:     permission.KindAllowDeny,
		Reason:   policyReason,
		ToolCall: call,
	})
	if err != nil {
		return false, "", err
	}
	if len(result.UpdatedInput) > 0 {
		raw, err := json.Marshal(result.UpdatedInput)
		if err != nil {
			return false, "", err
		}
		call.Input = raw
	}
	if result.Allow {
		return true, result.Reason, nil
	}
	reason := result.Reason
	if reason == "" {
		reason = string(result.Outcome)
	}
	return false, reason, nil
}

func (r *Runtime) evaluatePolicies(ctx context.Context, call agentkit.ToolCall) (agentkit.Decision, error) {
	input := agentkit.PolicyInput{ToolCall: &call}
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

func deniedResult(call agentkit.ToolCall, reason string, audit map[string]string) agentkit.ToolResult {
	if reason == "" {
		reason = "denied"
	}
	merged := map[string]string{"decision": "deny", "reason": reason}
	for k, v := range audit {
		merged[k] = v
	}
	return agentkit.ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: reason,
		Audit:   merged,
	}
}

func timeoutResult(call agentkit.ToolCall) agentkit.ToolResult {
	return agentkit.ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: "tool execution timed out",
		Audit:   map[string]string{"decision": "timeout"},
	}
}

func ResultText(result agentkit.ToolResult) string {
	return result.Content
}
