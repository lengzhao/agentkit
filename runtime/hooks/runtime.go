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
}

func init() {
	pluginkit.Register("hooks/runtime", New)
}

func New(_ Config, deps Deps) (*Runtime, error) {
	var beforeStep []agentkit.BeforeStepHook
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
		}
	}
	sort.Slice(beforeStep, func(i, j int) bool {
		if beforeStep[i].Order() == beforeStep[j].Order() {
			return false
		}
		return beforeStep[i].Order() < beforeStep[j].Order()
	})
	return &Runtime{beforeStep: beforeStep}, nil
}

func (r *Runtime) BeforeStep(ctx context.Context, in *agentkit.BeforeStep) error {
	for _, hook := range r.beforeStep {
		if err := hook.BeforeStep(ctx, in); err != nil {
			return err
		}
	}
	return nil
}

var _ agentkit.HookRuntime = (*Runtime)(nil)
