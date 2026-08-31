package config

import "strings"

func mergeInstanceMaps(base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overlay))
	for id, node := range base {
		out[id] = node
	}
	for id, overlayNode := range overlay {
		if baseNode, ok := base[id]; ok {
			out[id] = mergeNodes(baseNode, overlayNode)
			continue
		}
		out[id] = overlayNode
	}
	return out
}

func mergeNodes(base, overlay any) any {
	baseMap, bok := asStringMap(base)
	overlayMap, ook := asStringMap(overlay)
	if !bok || !ook {
		return overlay
	}
	if overlayUse, ok := overlayMap["use"]; ok {
		baseUse, hasBaseUse := baseMap["use"]
		if !hasBaseUse || overlayUse != baseUse {
			return overlay
		}
	}
	return deepMergeMaps(baseMap, overlayMap)
}

func deepMergeMaps(base, overlay map[string]any) map[string]any {
	out := cloneMap(base)

	// Phase 1: scalars, map deep-merge, list replace, null key delete.
	for key, value := range overlay {
		if strings.HasSuffix(key, "+") || strings.HasSuffix(key, "-") {
			continue
		}
		if value == nil {
			delete(out, key)
			continue
		}
		if existing, ok := asStringMap(out[key]); ok {
			if incoming, ok := asStringMap(value); ok {
				out[key] = deepMergeMaps(existing, incoming)
				continue
			}
		}
		out[key] = value
	}

	// Phase 2: list append (key+).
	for key, value := range overlay {
		if !strings.HasSuffix(key, "+") {
			continue
		}
		baseKey := strings.TrimSuffix(key, "+")
		out[baseKey] = appendListValues(out[baseKey], value)
	}

	// Phase 3: list remove by value (key-). Map key delete uses null in phase 1.
	for key, value := range overlay {
		if !strings.HasSuffix(key, "-") {
			continue
		}
		baseKey := strings.TrimSuffix(key, "-")
		out[baseKey] = removeListValues(out[baseKey], value)
	}

	return out
}

func removeListValues(base, toRemove any) any {
	baseList := toAnySlice(base)
	removeList := toAnySlice(toRemove)
	if len(baseList) == 0 || len(removeList) == 0 {
		return base
	}
	out := make([]any, 0, len(baseList))
	for _, item := range baseList {
		if listContainsValue(removeList, item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func listContainsValue(list []any, target any) bool {
	for _, item := range list {
		if item == target {
			return true
		}
		if s, ok := target.(string); ok {
			if other, ok := item.(string); ok && other == s {
				return true
			}
		}
	}
	return false
}

func appendListValues(base, extra any) any {
	baseList := toAnySlice(base)
	extraList := toAnySlice(extra)
	if len(baseList) == 0 {
		return extra
	}
	if len(extraList) == 0 {
		return base
	}
	out := make([]any, 0, len(baseList)+len(extraList))
	out = append(out, baseList...)
	out = append(out, extraList...)
	return out
}

func toAnySlice(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []string:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = item
		}
		return out
	default:
		if v == nil {
			return nil
		}
		return []any{v}
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func asStringMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}
