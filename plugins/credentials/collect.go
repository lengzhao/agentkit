package credentials

import "strings"

func collectEnvKeys(v any, out map[string]struct{}) {
	if out == nil {
		return
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if strings.HasPrefix(s, "env:") {
			if key := envKey(s); key != "" {
				out[key] = struct{}{}
			}
		}
	case map[string]any:
		for _, child := range t {
			collectEnvKeys(child, out)
		}
	case []any:
		for _, child := range t {
			collectEnvKeys(child, out)
		}
	}
}
