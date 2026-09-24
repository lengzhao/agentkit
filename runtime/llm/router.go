package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
)

type RouterDeps struct {
	Protocols []agentkit.LLMProvider `json:"protocols"`
	Default   agentkit.LLMProvider   `json:"default"`
}

type Router struct {
	index    map[string]agentkit.LLMProvider
	defaultP agentkit.LLMProvider
}

// NewRouter registers llm/router: route LLMRequest.Model to a protocol plugin by catalog id.
func NewRouter(_ struct{}, deps RouterDeps) (agentkit.LLMProvider, error) {
	if len(deps.Protocols) == 0 {
		return nil, fmt.Errorf("llm/router requires deps.protocols")
	}
	if deps.Default == nil {
		return nil, fmt.Errorf("llm/router requires deps.default")
	}
	foundDefault := false
	for _, p := range deps.Protocols {
		if p == deps.Default {
			foundDefault = true
			break
		}
	}
	if !foundDefault {
		return nil, fmt.Errorf("llm/router: deps.default must be one of deps.protocols")
	}
	index, err := indexCatalog(deps.Protocols, deps.Default)
	if err != nil {
		return nil, err
	}
	return &Router{index: index, defaultP: deps.Default}, nil
}

func (r *Router) Name() string { return "router" }

func (r *Router) Stream(ctx context.Context, req agentkit.LLMRequest) (agentkit.LLMStream, error) {
	p, err := r.resolve(req.Model)
	if err != nil {
		return nil, err
	}
	return p.Stream(ctx, req)
}

func (r *Router) resolve(model string) (agentkit.LLMProvider, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		if p, ok := r.index[model]; ok {
			return p, nil
		}
	}
	if r.defaultP != nil {
		return r.defaultP, nil
	}
	if model == "" {
		return nil, fmt.Errorf("llm/router: empty model and no default protocol")
	}
	return nil, fmt.Errorf("llm/router: unknown model %q and no default protocol", model)
}

func (r *Router) ModalitiesForModel(model string) []string {
	model = strings.TrimSpace(model)
	if model != "" {
		if p, ok := r.index[model]; ok {
			return ProviderModalitiesForModel(p, model)
		}
	}
	if r.defaultP != nil {
		return ProviderModalitiesForModel(r.defaultP, model)
	}
	return agentkit.NormalizeModalities(nil)
}
