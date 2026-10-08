package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	rtpermission "github.com/lengzhao/agentkit/runtime/permission"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

type RuntimeConfig struct {
	// DefaultTimeoutSeconds is per-call timeout when a tool has no specific entry.
	DefaultTimeoutSeconds int `json:"defaultTimeoutSeconds"`
	// MaxResultBytes is deprecated and ignored; spill/truncation happens in session.PrepareToolResultForStorage.
	MaxResultBytes int `json:"maxResultBytes"`
	// ToolTimeouts are per-tool timeout overrides, keyed by tool name (canonical or model-visible; normalized like ExposedToolName).
	ToolTimeouts map[string]int `json:"toolTimeouts,omitempty"`
	// AllowTools is a model-visible tool name whitelist. When non-empty, only listed tools are exposed (names normalized like ExposedToolName).
	AllowTools []string `json:"allowTools,omitempty"`
	// DenyTools is a model-visible tool name blacklist. Ignored when AllowTools is set (names normalized like ExposedToolName).
	DenyTools []string `json:"denyTools,omitempty"`
}

// Validate implements pluginkit.Validator.
func (c *RuntimeConfig) Validate() error {
	if c.DefaultTimeoutSeconds < 0 {
		return fmt.Errorf("tools/runtime defaultTimeoutSeconds must not be negative")
	}
	for name, seconds := range c.ToolTimeouts {
		if seconds <= 0 {
			return fmt.Errorf("tools/runtime toolTimeouts[%q] must be positive", name)
		}
	}
	return nil
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
	exposed          map[string]agentkit.Tool
	dynamicProviders []agentkit.ToolProvider
	dynamicTools     map[string]agentkit.Tool
	dynamicMu        sync.Mutex
	policies         []agentkit.Policy
	approval         agentkit.Approval
	hooks            agentkit.HookRuntime
	defaultTimeout   time.Duration
	toolTimeouts     map[string]time.Duration
	filter           toolNameFilter
	filterWarnOnce   sync.Once
}

// NewRuntime registers tools/runtime: Tool orchestration: visibility, policy evaluation, hooks, execution, result capping.
//
// Best practices:
//   - Policies are the enforcement plane; hooks only observe and rewrite. Put a security rule in a policy.
//   - The approval dep is consulted only for ask decisions, so it never sees an allowed or denied call.
func NewRuntime(cfg RuntimeConfig, deps RuntimeDeps) (agentkit.ToolRuntime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
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
		toolTimeouts[ExposedToolName(name)] = time.Duration(seconds) * time.Second
	}
	var defaultTimeout time.Duration
	if cfg.DefaultTimeoutSeconds > 0 {
		defaultTimeout = time.Duration(cfg.DefaultTimeoutSeconds) * time.Second
	}
	r := &Runtime{
		tools:            tools,
		dynamicProviders: deps.DynamicTools,
		dynamicTools:     make(map[string]agentkit.Tool),
		policies:         deps.Policies,
		approval:         deps.Approval,
		hooks:            deps.Hooks,
		defaultTimeout:   defaultTimeout,
		toolTimeouts:     toolTimeouts,
		filter:           newToolNameFilter(cfg.AllowTools, cfg.DenyTools),
	}
	r.rebuildExposedCatalog()
	return r, nil
}

// StaticToolNames returns tool names from deps.tools and deps.toolPacks only (not dynamic providers).
// tools/deferred uses this to decide which specs stay eager in the model-visible list.
func (r *Runtime) StaticToolNames() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, ExposedToolName(name))
	}
	sort.Strings(names)
	return names
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
		r.rebuildExposedCatalog()
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
				slog.Warn("tools/runtime: skipped dynamic tool; canonical name collides with static tool",
					"canonical_name", name,
				)
				continue
			}
			if _, ok := dynamic[name]; ok {
				slog.Warn("tools/runtime: skipped dynamic tool; duplicate canonical name",
					"canonical_name", name,
				)
				continue
			}
			dynamic[name] = tool
		}
	}
	r.dynamicMu.Lock()
	r.dynamicTools = dynamic
	r.dynamicMu.Unlock()
	r.rebuildExposedCatalog()
	return nil
}

func (r *Runtime) rebuildExposedCatalog() {
	r.dynamicMu.Lock()
	dynamic := r.dynamicTools
	r.dynamicMu.Unlock()
	exposed, dropped := buildExposedCatalog(r.tools, dynamic)
	if dropped > 0 {
		slog.Warn("tools/runtime: dropped tools with duplicate model-visible names", "count", dropped)
	}
	r.exposed = exposed
}

func (r *Runtime) Visible(ctx context.Context) ([]agentkit.ToolSpec, error) {
	if err := r.refreshDynamic(ctx); err != nil {
		return nil, err
	}
	specs := make([]agentkit.ToolSpec, 0, len(r.exposed))
	available := make(map[string]bool)
	for _, exposedName := range sortedKeys(r.exposed) {
		tool := r.exposed[exposedName]
		available[exposedName] = true
		specs = append(specs, agentkit.ToolSpec{
			Name:          exposedName,
			Description:   tool.Description(),
			InputSchema:   tool.InputSchema(),
			ExecutionMode: specExecutionMode(tool),
		})
	}
	r.filter.warnUnknownAllowNames(available, &r.filterWarnOnce)
	return filterToolSpecs(specs, r.filter), nil
}

func (r *Runtime) Execute(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	sessionID := rctx.SessionIDFromContext(ctx)
	agentID := rctx.AgentIDFromContext(ctx)
	return r.withToolObservation(ctx, call, func(ctx context.Context) (agentkit.ToolResult, error) {
		return r.execute(ctx, call, sessionID, agentID)
	})
}

func (r *Runtime) withToolObservation(ctx context.Context, call agentkit.ToolCall, run func(context.Context) (agentkit.ToolResult, error)) (agentkit.ToolResult, error) {
	ctx = context.WithValue(ctx, agentkit.KeyToolCallID, call.ID)
	ctx, endObservation := telemetry.BeginObservation(ctx, telemetry.ObservationMetaFromContext(ctx, captelemetry.ObservationMeta{
		Name:  "tool." + call.Name,
		Kind:  captelemetry.KindTool,
		Input: string(call.Input),
		Attributes: telemetry.MergeStringMaps(
			telemetry.ToolObservationAttrs(ctx, call),
			map[string]string{"tool_name": call.Name},
		),
	}))
	var observationEnd captelemetry.ObservationEnd
	defer func() {
		endObservation(observationEnd)
	}()

	result, err := run(ctx)
	if err != nil {
		if agentkit.IsTurnAbort(err) {
			observationEnd.Err = err
			return result, err
		}
		observationEnd.Output = err.Error()
		return result, err
	}
	observationEnd.Output = result.Content
	return result, nil
}

func (r *Runtime) execute(ctx context.Context, call agentkit.ToolCall, sessionID agentkit.SessionID, agentID agentkit.AgentID) (agentkit.ToolResult, error) {
	result, call, runBody, err := r.preflightTool(ctx, call, sessionID, agentID)
	if err != nil || !runBody {
		return result, err
	}
	return r.runToolBody(ctx, call, sessionID, agentID)
}

func (r *Runtime) lookupTool(callName string) (agentkit.Tool, bool) {
	if t, ok := r.exposed[callName]; ok {
		return t, true
	}
	if t, ok := r.tools[callName]; ok {
		return t, true
	}
	r.dynamicMu.Lock()
	t, ok := r.dynamicTools[callName]
	r.dynamicMu.Unlock()
	return t, ok
}

func (r *Runtime) timeoutFor(name string) time.Duration {
	if timeout, ok := r.toolTimeouts[name]; ok {
		return timeout
	}
	if timeout, ok := r.toolTimeouts[ExposedToolName(name)]; ok {
		return timeout
	}
	return r.defaultTimeout
}

func (r *Runtime) resolveAskDecision(ctx context.Context, call *agentkit.ToolCall, policyReason string) (bool, string, string, error) {
	if r.approval != nil {
		decision, err := r.approval.Ask(ctx, agentkit.ApprovalRequest{
			Reason:   policyReason,
			ToolCall: call,
		})
		if err != nil {
			return false, "", "", err
		}
		return decision.Allowed, decision.Reason, "", nil
	}

	broker, ok := rtpermission.BrokerFrom(ctx)
	if !ok {
		noHuman := rtpermission.NoHuman(permission.Request{
			Kind: permission.KindAllowDeny,
		}, "approval required but no permission broker on this session")
		return false, noHuman.Reason, noHuman.Guidance, nil
	}
	result, err := broker.Await(ctx, permission.Request{
		Kind:     permission.KindAllowDeny,
		Reason:   policyReason,
		ToolCall: call,
	})
	if err != nil {
		return false, "", "", err
	}
	if len(result.UpdatedInput) > 0 {
		raw, err := json.Marshal(result.UpdatedInput)
		if err != nil {
			return false, "", "", err
		}
		call.Input = raw
	}
	if result.Allow {
		return true, result.Reason, "", nil
	}
	reason := result.Reason
	if reason == "" {
		reason = string(result.Outcome)
	}
	return false, reason, result.Guidance, nil
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

func filteredOutResult(call agentkit.ToolCall) agentkit.ToolResult {
	return agentkit.ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: "tool not available",
		Audit:   map[string]string{"decision": "deny", "reason": "filtered by tools/runtime allow/deny list"},
	}
}

func deniedResult(call agentkit.ToolCall, reason, guidance string, audit map[string]string) agentkit.ToolResult {
	if reason == "" {
		reason = "denied"
	}
	merged := map[string]string{"decision": "deny", "reason": reason}
	if guidance != "" {
		merged["guidance"] = guidance
	}
	for k, v := range audit {
		merged[k] = v
	}
	return agentkit.ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: denialContent(reason, guidance),
		Audit:   merged,
	}
}

func denialContent(reason, guidance string) string {
	if guidance == "" {
		return reason
	}
	return reason + "\n\n" + guidance
}

func timeoutResult(call agentkit.ToolCall) agentkit.ToolResult {
	return agentkit.ToolResultFromTimeout(call)
}

func ResultText(result agentkit.ToolResult) string {
	return result.Content
}
