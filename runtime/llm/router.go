package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
	capllm "github.com/lengzhao/agentkit/cap/llm"
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
	stream, err := p.Stream(ctx, req)
	if err == nil && strings.TrimSpace(req.Model) != "" {
		NoteActualModel(ctx, req.Model)
	}
	return stream, err
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

// CatalogModels implements capllm.ModelCatalog so commands can list every
// routable model id across the indexed providers.
func (r *Router) CatalogModels() []capllm.ModelEntry {
	ids := make([]string, 0, len(r.index))
	for id := range r.index {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]capllm.ModelEntry, 0, len(ids))
	for _, id := range ids {
		out = append(out, capllm.ModelEntry{ID: id, Modalities: r.ModalitiesForModel(id)})
	}
	return out
}
