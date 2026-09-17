package credentials

import (
	"context"
	"os"
	"sort"
	"strings"

	capscredentials "github.com/lengzhao/agentkit/cap/credentials"
)

var baseProcessEnvKeys = map[string]struct{}{
	"PATH": {}, "HOME": {}, "USER": {}, "LOGNAME": {}, "SHELL": {},
	"LANG": {}, "LC_ALL": {}, "LC_CTYPE": {}, "TMPDIR": {}, "TZ": {}, "TERM": {},
	"XDG_CACHE_HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_DATA_HOME": {},
}

// BaseProcessExecEnv returns a minimal host environment for subprocesses (PATH, HOME, …).
func BaseProcessExecEnv(workDir string) []string {
	out := make([]string, 0, len(baseProcessEnvKeys)+1)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, keep := baseProcessEnvKeys[key]; keep {
			out = append(out, entry)
		}
	}
	if workDir != "" {
		out = append(out, "PWD="+workDir)
	}
	sort.Strings(out)
	return out
}

// InjectScopedEnv resolves env:KEY under scope for each key and appends KEY=value to base.
// Missing or empty secrets are skipped.
func InjectScopedEnv(ctx context.Context, base []string, scope string, keys []string, store capscredentials.Store) []string {
	if store == nil || len(keys) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(keys))
	out := append([]string(nil), base...)
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		ref := "env:" + key
		secret, err := store.Resolve(ctx, scope, ref)
		if err != nil || secret.Value == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key+"="+secret.Value)
	}
	return out
}
