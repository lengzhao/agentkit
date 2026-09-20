package workspace

import "strings"

// ParseScoped splits scoped paths such as "global:skills" or "local:.".
// Bare paths and absolute paths (~/foo, /abs) are not scoped.
func ParseScoped(rel string) (scope, path string, ok bool) {
	i := strings.Index(rel, ":")
	if i <= 0 {
		return "", rel, false
	}
	prefix := rel[:i]
	if prefix != ScopeGlobal && prefix != ScopeLocal {
		return "", rel, false
	}
	path = rel[i+1:]
	if path == "" {
		path = "."
	}
	return prefix, path, true
}
