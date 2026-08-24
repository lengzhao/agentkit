package config

import (
	"fmt"
	"os"
	"sort"

	"github.com/lengzhao/pluginkit/manager"
	"gopkg.in/yaml.v3"
)

const (
	DefaultBasePath    = "config.base.yaml"
	DefaultOverlayPath = "config.yaml"
	DefaultRootID      = "runner.default"
)

// LoadDocument loads L0 base config and optional L1 overlay, prunes unreachable instances, then parses.
func LoadDocument(basePath, overlayPath string) (manager.Document, error) {
	resolved, err := ResolveFiles(basePath, overlayPath)
	if err != nil {
		return manager.Document{}, err
	}
	return manager.FromYAML(resolved)
}

// ResolveFiles merges L0/L1 from disk and prunes instances unreachable from root.
func ResolveFiles(basePath, overlayPath string) ([]byte, error) {
	baseRaw, err := os.ReadFile(basePath)
	if err != nil {
		return nil, fmt.Errorf("read base config %q: %w", basePath, err)
	}

	var overlayRaw []byte
	if overlayPath != "" {
		overlayRaw, err = os.ReadFile(overlayPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read overlay config %q: %w", overlayPath, err)
		}
	}
	return ResolveYAML(baseRaw, overlayRaw)
}

// ResolveYAML merges base/overlay and prunes unreachable instances.
func ResolveYAML(base, overlay []byte) ([]byte, error) {
	merged, err := MergeYAML(base, overlay)
	if err != nil {
		return nil, err
	}
	raw, err := parseInstanceMap(merged)
	if err != nil {
		return nil, err
	}
	rootID, err := resolveRootID(raw)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(pruneToReachable(raw, rootID))
}

// MergeYAML merges two instance graphs. Overlay keys replace base entries entirely.
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
	for id, node := range overlayMap {
		baseMap[id] = node
	}
	return yaml.Marshal(baseMap)
}

// MergeFromFiles is an alias for ResolveFiles.
func MergeFromFiles(basePath, overlayPath string) ([]byte, error) {
	return ResolveFiles(basePath, overlayPath)
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
