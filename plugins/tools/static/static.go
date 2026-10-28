// Package static exposes a fixed set of already-constructed tools as an
// agentkit.ToolProvider.
//
// It exists so any tool or tool pack can be served to a remote consumer that
// speaks ToolProvider - notably agent/acp-remote's sessionMcp slot, which needs
// a provider to announce tools to an external ACP agent. Without it, every
// consumer had to hand-roll a provider wrapper for the same list.
package static

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit"
)

func init() {
	pluginkit.Register("tools/static", New)
}

// Config carries no knobs; the tool list lives in Deps so it can reference
// other graph instances.
type Config struct{}

// Deps lists the tools to expose. Nil entries are skipped.
type Deps struct {
	Tools []agentkit.Tool `json:"tools"`
}

// Provider is a ToolProvider over a fixed tool slice.
type Provider struct {
	tools []agentkit.Tool
}

// New registers tools/static: publish a fixed tool list as a ToolProvider.
// An empty list yields an empty provider rather than an error.
func New(_ Config, deps Deps) (agentkit.ToolProvider, error) {
	tools := make([]agentkit.Tool, 0, len(deps.Tools))
	for _, tool := range deps.Tools {
		if tool == nil {
			continue
		}
		tools = append(tools, tool)
	}
	// An empty list is legal: a referenced tool pack may have been pruned (a
	// missing CLI, missing credentials). Failing here would abort the whole
	// config build for a host whose other instances are perfectly fine.
	return &Provider{tools: tools}, nil
}

// ListTools returns the configured tools, ignoring the call context: the set is
// static by construction.
func (p *Provider) ListTools(context.Context) ([]agentkit.Tool, error) {
	out := make([]agentkit.Tool, len(p.tools))
	copy(out, p.tools)
	return out, nil
}
