package tools

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit"
)

// ExposedToolName makes a plugin tool name safe for OpenAI function / Responses API
// input names (^ [a-zA-Z0-9_-]+$). Non [A-Za-z0-9_] runes become '_'.
func ExposedToolName(canonical string) string {
	var b strings.Builder
	for _, r := range canonical {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "tool"
	}
	return out
}

type catalogOwner struct {
	Canonical string
}

type catalogRegistrar struct {
	taken   map[string]catalogOwner
	dropped int
}

func newCatalogRegistrar() *catalogRegistrar {
	return &catalogRegistrar{taken: make(map[string]catalogOwner)}
}

// register returns the exposed name and whether the tool should appear in the model catalog.
func (r *catalogRegistrar) register(canonical string, tool agentkit.Tool) (exposed string, include bool) {
	if tool == nil {
		return "", false
	}
	if canonical == "" {
		canonical = tool.Name()
	}
	exposed = ExposedToolName(canonical)
	prev, exists := r.taken[exposed]
	if exists {
		if prev.Canonical == canonical {
			return exposed, false
		}
		slog.Warn("tools/runtime: dropped tool with duplicate model-visible name",
			"exposed_name", exposed,
			"canonical_name", canonical,
			"kept_canonical_name", prev.Canonical,
		)
		r.dropped++
		return "", false
	}
	r.taken[exposed] = catalogOwner{Canonical: canonical}
	return exposed, true
}

func buildExposedCatalog(static map[string]agentkit.Tool, dynamic map[string]agentkit.Tool) (map[string]agentkit.Tool, int) {
	reg := newCatalogRegistrar()
	exposed := make(map[string]agentkit.Tool)

	add := func(canonical string, tool agentkit.Tool) {
		name, include := reg.register(canonical, tool)
		if !include {
			return
		}
		exposed[name] = tool
	}

	for _, name := range sortedKeys(static) {
		add(name, static[name])
	}
	for _, name := range sortedKeys(dynamic) {
		add(name, dynamic[name])
	}
	return exposed, reg.dropped
}

func sortedKeys(m map[string]agentkit.Tool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
