package llm

import (
	"fmt"
	"strings"

	capllm "github.com/lengzhao/agentkit/cap/llm"
	"github.com/lengzhao/agentkit"
)

// ModelCatalogEntry is config.models[] on protocol LLM plugins.
type ModelCatalogEntry struct {
	ID         string   `json:"id"`
	Modalities []string `json:"modalities,omitempty"`
}

func catalogFromConfig(entries []ModelCatalogEntry, defaultModalities []string) (map[string][]string, []capllm.ModelEntry, error) {
	if len(entries) == 0 {
		return nil, nil, nil
	}
	byID := make(map[string][]string, len(entries))
	out := make([]capllm.ModelEntry, 0, len(entries))
	for _, e := range entries {
		id := strings.TrimSpace(e.ID)
		if id == "" {
			continue
		}
		if _, dup := byID[id]; dup {
			return nil, nil, fmt.Errorf("duplicate model id %q in config.models", id)
		}
		mods := agentkit.NormalizeModalities(e.Modalities)
		if len(e.Modalities) == 0 && len(defaultModalities) > 0 {
			mods = agentkit.NormalizeModalities(defaultModalities)
		}
		byID[id] = mods
		out = append(out, capllm.ModelEntry{ID: id, Modalities: mods})
	}
	return byID, out, nil
}

func indexCatalog(protocols []agentkit.LLMProvider, skip agentkit.LLMProvider) (map[string]agentkit.LLMProvider, error) {
	index := make(map[string]agentkit.LLMProvider)
	for _, p := range protocols {
		if p == nil {
			continue
		}
		if skip != nil && p == skip {
			continue
		}
		cat, ok := p.(capllm.ModelCatalog)
		if !ok {
			continue
		}
		for _, e := range cat.CatalogModels() {
			id := strings.TrimSpace(e.ID)
			if id == "" {
				continue
			}
			if prev, exists := index[id]; exists {
				return nil, fmt.Errorf("llm/router: duplicate model id %q (%s and %s)", id, prev.Name(), p.Name())
			}
			index[id] = p
		}
	}
	return index, nil
}

func modalitiesForCatalog(byID map[string][]string, fallback []string, model string) []string {
	model = strings.TrimSpace(model)
	if byID != nil {
		if mods, ok := byID[model]; ok {
			return append([]string(nil), mods...)
		}
	}
	return append([]string(nil), fallback...)
}
