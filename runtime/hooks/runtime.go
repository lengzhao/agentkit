package hooks

import (
	"context"
	"sort"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	Providers []agentkit.HookProvider `json:"providers,omitempty"`
}

type Runtime struct {
	beforeStep []agentkit.BeforeStepHook
	beforeTool []agentkit.BeforeToolHook
	afterTool  []agentkit.AfterToolHook
}

func init() {
	pluginkit.Register("hooks/runtime", New)
}

func New(_ Config, deps Deps) (agentkit.HookRuntime, error) {
	var beforeStep []agentkit.BeforeStepHook
	var beforeTool []agentkit.BeforeToolHook
	var afterTool []agentkit.AfterToolHook
	for _, provider := range deps.Providers {
		if provider == nil {
			continue
		}
		for _, hook := range provider.Hooks() {
			if hook == nil {
				continue
			}
			if h, ok := hook.(agentkit.BeforeStepHook); ok {
				beforeStep = append(beforeStep, h)
			}
			if h, ok := hook.(agentkit.BeforeToolHook); ok {
				beforeTool = append(beforeTool, h)
			}
			if h, ok := hook.(agentkit.AfterToolHook); ok {
				afterTool = append(afterTool, h)
			}
		}
	}
	sortHooks(beforeStep)
	sortHooks(beforeTool)
	sortHooks(afterTool)
	return &Runtime{
		beforeStep: beforeStep,
		beforeTool: beforeTool,
		afterTool:  afterTool,
	}, nil
}

func sortHooks[H agentkit.Hook](hooks []H) {
	sort.Slice(hooks, func(i, j int) bool {
		if hooks[i].Order() == hooks[j].Order() {
			return false
		}
		return hooks[i].Order() < hooks[j].Order()
	})
}

func (r *Runtime) BeforeStep(ctx context.Context, in *agentkit.BeforeStep) error {
	for _, hook := range r.beforeStep {
		if err := hook.BeforeStep(ctx, in); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) BeforeTool(ctx context.Context, in *agentkit.ToolCall) error {
	for _, hook := range r.beforeTool {
		if err := hook.BeforeTool(ctx, in); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) AfterTool(ctx context.Context, in *agentkit.ToolResult) error {
	for _, hook := range r.afterTool {
		if err := hook.AfterTool(ctx, in); err != nil {
			return err
		}
	}
	return nil
}

var _ agentkit.HookRuntime = (*Runtime)(nil)
