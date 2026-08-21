package credentials

import "strings"

// EnvKey normalizes refs like "env:OPENAI_API_KEY" to "OPENAI_API_KEY".
func EnvKey(ref string) string {
	ref = strings.TrimSpace(ref)
	if after, ok := strings.CutPrefix(ref, "env:"); ok {
		return after
	}
	return ref
}
