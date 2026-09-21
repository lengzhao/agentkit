package workspace

import (
	"fmt"
	"strings"
)

// ParseScoped splits configuration paths such as "global:skills" or "local:.".
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

// IsScoped reports whether rel uses a configuration-only global:/local: prefix.
func IsScoped(rel string) bool {
	_, _, ok := ParseScoped(rel)
	return ok
}

// FirstScoped returns the first entry in files whose global:/local: prefix
// matches scope. For ScopeLocal, a bare path (no colon) is used when no
// local: entry exists. Other colon-bearing strings (URLs, Windows drives)
// are not treated as bare paths.
func FirstScoped(files []string, scope string) (string, error) {
	if scope != ScopeGlobal && scope != ScopeLocal {
		return "", fmt.Errorf("unknown scope %q", scope)
	}
	for _, f := range files {
		f = strings.TrimSpace(f)
		got, _, ok := ParseScoped(f)
		if ok && got == scope {
			return f, nil
		}
	}
	if scope == ScopeGlobal {
		return "", fmt.Errorf("no global config file configured")
	}
	for _, f := range files {
		if f = strings.TrimSpace(f); f != "" && !strings.Contains(f, ":") {
			return f, nil
		}
	}
	return "", fmt.Errorf("no local config file configured")
}
