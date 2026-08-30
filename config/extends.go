package config

import (
	"fmt"
	"strings"
)

func expandExtends(raw map[string]any) (map[string]any, error) {
	cache := make(map[string]map[string]any, len(raw))
	out := make(map[string]any, len(raw))
	for id, node := range raw {
		resolved, err := resolveExtendsNode(id, node, raw, nil, cache)
		if err != nil {
			return nil, err
		}
		out[id] = resolved
	}
	return out, nil
}

func resolveExtendsNode(id string, node any, raw map[string]any, visiting map[string]bool, cache map[string]map[string]any) (map[string]any, error) {
	if cached, ok := cache[id]; ok {
		return cached, nil
	}
	nodeMap, ok := asStringMap(node)
	if !ok {
		return nil, fmt.Errorf("instance %q: node must be an object", id)
	}

	extends, hasExtends := nodeMap["extends"].(string)
	if !hasExtends {
		resolved := cloneMap(nodeMap)
		cache[id] = resolved
		return resolved, nil
	}
	extends = strings.TrimSpace(extends)
	if extends == "" {
		return nil, fmt.Errorf("instance %q: extends must be a non-empty instance id", id)
	}
	if visiting == nil {
		visiting = make(map[string]bool)
	}
	if visiting[id] {
		return nil, fmt.Errorf("instance %q: extends cycle detected", id)
	}
	visiting[id] = true
	defer delete(visiting, id)

	parentRaw, ok := raw[extends]
	if !ok {
		return nil, fmt.Errorf("instance %q: extends target %q not found", id, extends)
	}
	parent, err := resolveExtendsNode(extends, parentRaw, raw, visiting, cache)
	if err != nil {
		return nil, err
	}

	child := cloneMap(nodeMap)
	delete(child, "extends")
	merged, ok := mergeNodes(parent, child).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("instance %q: failed to merge extends target %q", id, extends)
	}
	cache[id] = merged
	return merged, nil
}
