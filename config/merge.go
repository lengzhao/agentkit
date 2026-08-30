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
	for key, value := range overlay {
		if strings.HasSuffix(key, "+") {
			baseKey := strings.TrimSuffix(key, "+")
			out[baseKey] = appendListValues(out[baseKey], value)
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
	return out
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
