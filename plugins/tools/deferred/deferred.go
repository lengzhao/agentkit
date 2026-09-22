package deferred

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lengzhao/agentkit"
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
	split := d.classifyVisible(specs)
	if len(split.Deferrable) == 0 || !d.cfg.disclosureActive(split.Deferrable, 0) {
		return specs, nil
	}
	return assembleVisible(split.Eager, split.Deferrable, d.cfg, 0), nil
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

func (d *Runtime) Execute(ctx context.Context, call agentkit.ToolCall) (agentkit.ToolResult, error) {
	if !d.bridgesActive(ctx) {
		return d.inner.Execute(ctx, call)
	}
	switch call.Name {
	case ToolSearch:
		return d.executeSearch(ctx, call)
	case ToolDescribe:
		return d.executeDescribe(ctx, call)
	case ToolCall:
		return d.executeBridgeCall(ctx, call)
	default:
		return d.inner.Execute(ctx, call)
	}
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
