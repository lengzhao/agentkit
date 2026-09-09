package tools

import (
	"log/slog"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
)

// toolNameFilter applies allow/deny lists to model-visible tool names.
// When allow is non-empty it acts as a whitelist; otherwise deny is a blacklist.
// Semantics match tool/mcp and tool/openapi source-level filters.
type toolNameFilter struct {
	allow     map[string]struct{}
	deny      map[string]struct{}
	allowMode bool
}

func newToolNameFilter(allow, deny []string) toolNameFilter {
	f := toolNameFilter{}
	for _, name := range allow {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if f.allow == nil {
			f.allow = make(map[string]struct{})
		}
		f.allow[name] = struct{}{}
	}
	f.allowMode = len(f.allow) > 0
	for _, name := range deny {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if f.deny == nil {
			f.deny = make(map[string]struct{})
		}
		f.deny[name] = struct{}{}
	}
	return f
}

func (f toolNameFilter) active() bool {
	return f.allowMode || len(f.deny) > 0
}

func (f toolNameFilter) allows(name string) bool {
	if f.allowMode {
		_, ok := f.allow[name]
		return ok
	}
	if len(f.deny) > 0 {
		_, denied := f.deny[name]
		return !denied
	}
	return true
}

func (f toolNameFilter) warnUnknownAllowNames(available map[string]bool, once *sync.Once) {
	if !f.allowMode {
		return
	}
	once.Do(func() {
		var unknown []string
		for name := range f.allow {
			if !available[name] {
				unknown = append(unknown, name)
			}
		}
		if len(unknown) > 0 {
			slog.Warn("tools/runtime allowTools has unknown names", "unknown", strings.Join(unknown, ","))
		}
	})
}

func filterToolSpecs(specs []agentkit.ToolSpec, f toolNameFilter) []agentkit.ToolSpec {
	if !f.active() {
		return specs
	}
	out := make([]agentkit.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if f.allows(spec.Name) {
			out = append(out, spec)
		}
	}
	return out
}
