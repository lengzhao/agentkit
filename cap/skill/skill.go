package skill

import "context"

type Registry interface {
	List(context.Context) ([]Descriptor, error)
	Load(context.Context, string) (Content, error)
}

type Descriptor struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Path          string `json:"path"`
	License       string `json:"license,omitempty"`
	Compatibility string `json:"compatibility,omitempty"`
}

type Content struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Body          string            `json:"body"`
	Path          string            `json:"path"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	AllowedTools  string            `json:"allowedTools,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}
