package deferred

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/telemetry"
	"github.com/lengzhao/agentkit/runtime/tools"
)

// Deps are the same slots as tools/runtime (hooks, tools, toolPacks, dynamicTools, policies, approval).
type Deps = tools.RuntimeDeps

// Runtime wraps tools/runtime with progressive disclosure for dynamic tools.
type Runtime struct {
	inner      agentkit.ToolRuntime
	cfg        DisclosureConfig
	eager      map[string]bool
	deferForce map[string]bool
}

type staticToolNames interface {
	StaticToolNames() []string
}

// New registers tools/deferred: same config/deps shape as tools/runtime; builds runtime internally.
//
// Set config.enabled true to replace dynamic tool schemas with tool_search / tool_describe / tool_call.
func New(cfg Config, deps Deps) (agentkit.ToolRuntime, error) {
	cfg = cfg.Normalize()
	inner, err := tools.NewRuntime(cfg.RuntimeConfig, deps)
	if err != nil {
		return nil, err
	}
	if err := checkNoBridgeNameCollision(inner); err != nil {
		return nil, err
	}
	d := &Runtime{
		inner: inner,
		cfg:   cfg.DisclosureConfig,
	}
	d.buildEagerSets()
	return d, nil
}

func checkNoBridgeNameCollision(inner agentkit.ToolRuntime) error {
	specs, err := inner.Visible(context.Background())
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if IsBridge(spec.Name) {
			return fmt.Errorf("tools/deferred: inner catalog has tool %q which conflicts with bridge tools", spec.Name)
		}
	}
	return nil
}

func (d *Runtime) buildEagerSets() {
	eager := make(map[string]bool)
	if st, ok := d.inner.(staticToolNames); ok {
		for _, name := range st.StaticToolNames() {
			eager[name] = true
		}
	} else {
		slog.Warn("tools/deferred inner does not expose StaticToolNames; only config.eagerTools mark tools eager")
	}
	for _, name := range d.cfg.EagerTools {
		name = strings.TrimSpace(name)
		if name != "" {
			eager[tools.ExposedToolName(name)] = true
		}
	}
	deferForce := make(map[string]bool)
	for _, name := range d.cfg.DeferTools {
		name = strings.TrimSpace(name)
		if name != "" {
			deferForce[tools.ExposedToolName(name)] = true
		}
	}
	d.eager = eager
	d.deferForce = deferForce
}

func (d *Runtime) Visible(ctx context.Context) ([]agentkit.ToolSpec, error) {
	specs, err := d.inner.Visible(ctx)
	if err != nil {
		return nil, err
	}
	// Activation is decided on the pre-reveal deferrable set: reveal only affects
	// assembly, never flips disclosure off (which would dump every schema at once).
	split := d.classifyVisible(specs)
	if len(split.Deferrable) == 0 || !d.cfg.disclosureActive(split.Deferrable, 0) {
		return specs, nil
	}
	split = d.applyReveal(ctx, split)
	return assembleVisible(split.Eager, split.Deferrable, d.cfg, 0), nil
}

// applyReveal promotes turn-revealed deferrable tools (search/describe hits)
// into the eager set so the model can call them directly on later steps.
func (d *Runtime) applyReveal(ctx context.Context, split classified) classified {
	if len(split.Deferrable) == 0 {
		return split
	}
	revealed := revealedNames(ctx)
	if len(revealed) == 0 {
		return split
	}
	eager := make([]agentkit.ToolSpec, 0, len(split.Eager)+len(revealed))
	eager = append(eager, split.Eager...)
	rest := make([]agentkit.ToolSpec, 0, len(split.Deferrable))
	for _, spec := range split.Deferrable {
		if revealed[spec.Name] {
			eager = append(eager, spec)
		} else {
			rest = append(rest, spec)
		}
	}
	return classified{Eager: eager, Deferrable: rest}
}

func (d *Runtime) classifyVisible(specs []agentkit.ToolSpec) classified {
	if len(d.eager) == 0 && len(d.deferForce) == 0 {
		slog.Warn("tools/deferred cannot classify tools; treating all as eager")
		return classified{Eager: specs}
	}
	return classify(specs, d.eager, d.deferForce)
}

func (d *Runtime) bridgesActive(ctx context.Context) bool {
	specs, err := d.inner.Visible(ctx)
	if err != nil {
		return false
	}
	split := d.classifyVisible(specs)
	return len(split.Deferrable) > 0 && d.cfg.disclosureActive(split.Deferrable, 0)
}

// PreflightTool implements agentkit.ToolBatchRuntime. Bridge tools
// (tool_search / tool_describe / tool_call) exist only in this wrapper — the
// inner runtime catalog has no such names, so delegating their preflight would
// deny them with "tool not found". Bridges are catalog-level operations
// handled entirely here during preflight (runBody=false); policy/hooks for the
// real tool behind tool_call still run inside inner.Execute.
// Eager tools and revealed deferred tools (called directly after search)
// delegate to the inner preflight pipeline unchanged.
func (d *Runtime) PreflightTool(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, agentkit.ToolCall, bool, error) {
	if IsBridge(call.Name) && d.bridgesActive(ctx) {
		result, err := d.Execute(ctx, call)
		return result, call, false, err
	}
	if batch, ok := d.inner.(agentkit.ToolBatchRuntime); ok {
		return batch.PreflightTool(ctx, call)
	}
	result, err := d.Execute(ctx, call)
	return result, call, false, err
}

func (d *Runtime) RunToolBody(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	// Defensive: bridges normally finish in PreflightTool (runBody=false).
	if IsBridge(call.Name) && d.bridgesActive(ctx) {
		return d.Execute(ctx, call)
	}
	if batch, ok := d.inner.(agentkit.ToolBatchRuntime); ok {
		return batch.RunToolBody(ctx, call)
	}
	return agentkit.ToolResult{}, fmt.Errorf("tool runtime does not support batched body execution")
}

func (d *Runtime) Execute(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	if !d.bridgesActive(ctx) {
		return d.inner.Execute(ctx, call)
	}
	switch call.Name {
	case ToolSearch:
		return d.executeBridgeObserved(ctx, call, "tool.deferred.search", nil, d.executeSearch)
	case ToolDescribe:
		return d.executeBridgeObserved(ctx, call, "tool.deferred.describe", nil, d.executeDescribe)
	case ToolCall:
		if !d.cfg.callBridgeEnabled() {
			return bridgeError(call, "tool_call bridge is disabled (callBridge: off); invoke the revealed tool directly by name"), nil
		}
		extra := map[string]string{}
		if entries, err := normalizeCalls(call.Input); err == nil && len(entries) > 0 {
			extra["deferred_target"] = entries[0].Name
		}
		return d.executeBridgeObserved(ctx, call, "tool.deferred.call", extra, d.executeBridgeCall)
	default:
		return d.inner.Execute(ctx, call)
	}
}

// executeBridgeObserved records a telemetry observation for bridge tools, mirroring
// tools/runtime.Execute so tool_search / tool_describe / tool_call show up in traces.
// The real tool invoked through tool_call still gets its own child span from inner.Execute.
func (d *Runtime) executeBridgeObserved(ctx context.Context, call agentkit.ToolCall, spanName string, extra map[string]string, handler func(context.Context, agentkit.ToolCall) (agentkit.ToolResult, error)) (agentkit.ToolResult, error) {
	ctx = context.WithValue(ctx, agentkit.KeyToolCallID, call.ID)
	ctx, endObservation := telemetry.BeginObservation(ctx, telemetry.ObservationMetaFromContext(ctx, captelemetry.ObservationMeta{
		Name:  spanName,
		Kind:  captelemetry.KindTool,
		Input: string(call.Input),
		Attributes: telemetry.MergeStringMaps(
			telemetry.ToolObservationAttrs(ctx, call),
			extra,
			map[string]string{"tool_name": call.Name},
		),
	}))
	var observationEnd captelemetry.ObservationEnd
	defer func() {
		endObservation(observationEnd)
	}()

	result, err := handler(ctx, call)
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

func (d *Runtime) deferrableCatalog(ctx context.Context) ([]catalogEntry, error) {
	specs, err := d.inner.Visible(ctx)
	if err != nil {
		return nil, err
	}
	split := classify(specs, d.eager, d.deferForce)
	return buildCatalog(split.Deferrable), nil
}

func (d *Runtime) catalogNameSet(ctx context.Context) (map[string]bool, error) {
	catalog, err := d.deferrableCatalog(ctx)
	if err != nil {
		return nil, err
	}
	return catalogNamesSet(catalog), nil
}

func bridgeError(call agentkit.ToolCall, msg string) agentkit.ToolResult {
	return agentkit.ToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: msg,
		Audit:   map[string]string{"decision": "deny", "reason": msg},
	}
}
