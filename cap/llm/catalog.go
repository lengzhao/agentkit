package llm

// ModelEntry declares a routable model id and optional input modalities for that id.
type ModelEntry struct {
	ID         string   `json:"id"`
	Modalities []string `json:"modalities,omitempty"`
}

// ModelCatalog is implemented by protocol LLM plugins that participate in llm/router
// exact-match routing. Instances used only as router default may still implement
// ModelCatalog for per-model modalities metadata.
type ModelCatalog interface {
	CatalogModels() []ModelEntry
}
