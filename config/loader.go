package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lengzhao/pluginkit/manager"
	"gopkg.in/yaml.v3"
)

const (
	DefaultBasePath    = "config.base.yaml"
	DefaultOverlayPath = "config.yaml"
	DefaultRootID      = "runner.default"
)

// LoadDocument loads L0 base config and optional L1 overlays, prunes unreachable
// instances, then parses. Overlays apply in order, so a later one wins.
func LoadDocument(basePath string, overlayPaths ...string) (manager.Document, error) {
	resolved, err := ResolveFiles(basePath, overlayPaths...)
	if err != nil {
		return manager.Document{}, err
	}
	return manager.FromYAML(resolved)
}

// ResolveFiles merges L0 with each L1 overlay in order and prunes instances
// unreachable from root. Chaining lets a capability stack live in one preset and
// a transport swap in another, instead of every preset restating the whole graph.
func ResolveFiles(basePath string, overlayPaths ...string) ([]byte, error) {
	baseRaw, err := os.ReadFile(basePath)
	if err != nil {
		return nil, fmt.Errorf("read base config %q: %w", basePath, err)
	}

	overlays := make([][]byte, 0, len(overlayPaths))
	for _, path := range overlayPaths {
		if path == "" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			// A missing overlay is not fatal: the default config.yaml is optional.
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read overlay config %q: %w", path, err)
		}
		overlays = append(overlays, raw)
	}
	interpDir := filepath.Dir(basePath)
	for i := len(overlayPaths) - 1; i >= 0; i-- {
		path := overlayPaths[i]
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			interpDir = filepath.Dir(path)
			break
		}
	}
	return ResolveYAML(interpDir, baseRaw, overlays...)
}

// ResolveYAML merges base with each overlay in order, expands extends, interpolates
// string values, then prunes unreachable instances.
func ResolveYAML(interpDir string, base []byte, overlays ...[]byte) ([]byte, error) {
	merged := base
	for _, overlay := range overlays {
		next, err := MergeYAML(merged, overlay)
		if err != nil {
			return nil, err
		}
		merged = next
	}
	raw, err := parseInstanceMap(merged)
	if err != nil {
		return nil, err
	}
	raw, err = expandExtends(raw)
	if err != nil {
		return nil, err
	}
	raw, err = pruneUnavailableInstances(raw, interpDir)
	if err != nil {
		return nil, err
	}
	if err := interpolateInstances(raw, interpDir); err != nil {
		return nil, err
	}
	rootID, err := resolveRootID(raw)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(pruneToReachable(raw, rootID))
}

// MergeYAML merges two instance graphs. When overlay changes use, the whole node
// is replaced; otherwise config and deps are deep-merged into the base node.
func MergeYAML(base, overlay []byte) ([]byte, error) {
	baseMap, err := parseInstanceMap(base)
	if err != nil {
		return nil, fmt.Errorf("parse base: %w", err)
	}
	if len(overlay) == 0 {
		return base, nil
	}
	overlayMap, err := parseInstanceMap(overlay)
	if err != nil {
		return nil, fmt.Errorf("parse overlay: %w", err)
	}
	merged := mergeInstanceMaps(baseMap, overlayMap)
	return yaml.Marshal(merged)
}

// MergeFromFiles is an alias for ResolveFiles.
func MergeFromFiles(basePath string, overlayPaths ...string) ([]byte, error) {
	return ResolveFiles(basePath, overlayPaths...)
}

// SplitOverlayPaths parses a comma-separated overlay list, e.g.
// "presets/autonomous.yaml,presets/daemon.yaml".
func SplitOverlayPaths(spec string) []string {
	var out []string
	for _, part := range strings.Split(spec, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseInstanceMap(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("empty yaml")
	}
	return m, nil
}

func resolveRootID(raw map[string]any) (string, error) {
	if _, ok := raw[DefaultRootID]; ok {
		return DefaultRootID, nil
	}

	referenced := map[string]bool{}
	for _, node := range raw {
		collectReferencedFromNode(node, referenced)
	}
	unreferenced := make([]string, 0)
	for id := range raw {
		if !referenced[id] {
			unreferenced = append(unreferenced, id)
		}
	}
	sort.Strings(unreferenced)
	switch len(unreferenced) {
	case 1:
		return unreferenced[0], nil
	default:
		return "", fmt.Errorf("cannot determine root: multiple unreferenced instances %v", unreferenced)
	}
}

func collectReferencedFromNode(node any, referenced map[string]bool) {
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	deps, ok := m["deps"].(map[string]any)
	if !ok {
		return
	}
	for _, ref := range deps {
		collectDepRefs(ref, referenced)
	}
}

func collectDepRefs(v any, referenced map[string]bool) {
	walkDepRefs(v, func(id string) {
		referenced[id] = true
	})
}

func pruneToReachable(raw map[string]any, rootID string) map[string]any {
	reachable := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if reachable[id] {
			return
		}
		node, ok := raw[id]
		if !ok {
			return
		}
		reachable[id] = true
		m, ok := node.(map[string]any)
		if !ok {
			return
		}
		deps, ok := m["deps"].(map[string]any)
		if !ok {
			return
		}
		for _, ref := range deps {
			walkDepRefs(ref, walk)
		}
	}
	walk(rootID)

	out := make(map[string]any, len(reachable))
	for id := range reachable {
		out[id] = raw[id]
	}
	return out
}

func walkDepRefs(v any, visit func(string)) {
	switch x := v.(type) {
	case string:
		if x != "" {
			visit(x)
		}
	case map[string]any:
		if _, ok := x["use"]; ok {
			if deps, ok := x["deps"].(map[string]any); ok {
				for _, ref := range deps {
					walkDepRefs(ref, visit)
				}
			}
			return
		}
	case []any:
		for _, item := range x {
			walkDepRefs(item, visit)
		}
	case []string:
		for _, id := range x {
			if id != "" {
				visit(id)
			}
		}
	}
}
