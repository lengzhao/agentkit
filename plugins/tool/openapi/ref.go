package openapi

import "strings"

func envKey(ref string) string {
	ref = strings.TrimSpace(ref)
	if after, ok := strings.CutPrefix(ref, "env:"); ok {
		return after
	}
	return ref
}
