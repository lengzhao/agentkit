package telemetry

// MergeStringMaps copies attrs into a new map, then overlays extra (extra wins on key clash).
func MergeStringMaps(extra map[string]string, attrs ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range attrs {
		for k, v := range m {
			if v != "" {
				out[k] = v
			}
		}
	}
	for k, v := range extra {
		if v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// TruncateMeta shortens long strings for exporter metadata fields.
func TruncateMeta(s string) string {
	return truncateMeta(s)
}
