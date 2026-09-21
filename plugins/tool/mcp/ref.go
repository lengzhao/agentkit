package mcp

import "strings"

func envKey(ref string) string {
	ref = strings.TrimSpace(ref)
	if after, ok := strings.CutPrefix(ref, "env:"); ok {
		return after
	}
	return ref
}

func collectEnvKey(v string, out map[string]struct{}) {
	s := strings.TrimSpace(v)
	if !strings.HasPrefix(s, "env:") {
		return
	}
	if key := envKey(s); key != "" {
		out[key] = struct{}{}
	}
}
