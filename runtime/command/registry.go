package command

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/cap/command"
	"github.com/lengzhao/pluginkit"
)

type Config struct{}

type Deps struct {
	Handlers []command.Handler `json:"handlers,omitempty"`
}

type Registry struct {
	byName map[string]command.Handler
	descs  []command.Descriptor
}

func init() {
	pluginkit.Register("command/registry", New)
}

func New(_ Config, deps Deps) (command.Registry, error) {
	r := &Registry{byName: make(map[string]command.Handler)}
	for _, handler := range deps.Handlers {
		if handler == nil {
			continue
		}
		registrar, ok := handler.(commandRegistrar)
		if !ok {
			return nil, fmt.Errorf("command handler %T must implement Descriptor()", handler)
		}
		if err := r.Register(registrar.Descriptor(), handler); err != nil {
			return nil, err
		}
	}
	return r, nil
}

type commandRegistrar interface {
	Descriptor() command.Descriptor
}

func (r *Registry) Register(desc command.Descriptor, handler command.Handler) error {
	name := normalizeName(desc.Name)
	if name == "" {
		return fmt.Errorf("command name is required")
	}
	if handler == nil {
		return fmt.Errorf("command handler is required")
	}
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("command already registered: %s", name)
	}
	r.byName[name] = handler
	r.descs = append(r.descs, desc)
	for _, alias := range desc.Aliases {
		key := normalizeName(alias)
		if key == "" {
			continue
		}
		if _, exists := r.byName[key]; exists {
			return fmt.Errorf("command alias already registered: %s", key)
		}
		r.byName[key] = handler
	}
	return nil
}

func (r *Registry) Dispatch(ctx context.Context, req command.Request) (command.Result, error) {
	name := normalizeName(req.Name)
	if name == "" {
		return command.Result{}, nil
	}
	handler, ok := r.byName[name]
	if !ok {
		return command.Result{}, nil
	}
	result, err := handler.Handle(ctx, req)
	if err != nil {
		return command.Result{}, err
	}
	result.Handled = true
	return result, nil
}

func (r *Registry) List() []command.Descriptor {
	out := make([]command.Descriptor, len(r.descs))
	copy(out, r.descs)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

var _ command.Registry = (*Registry)(nil)
