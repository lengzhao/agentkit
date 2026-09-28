package shell

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/cap/credentials"
)

// subprocessEnv builds the bash child-process environment. By default the child
// inherits the full host env (os.Environ) plus scoped pairs resolved from the
// credentials store. trimEnv=true switches to a trimmed base (PATH/HOME/... +
// PWD) plus scoped/extra pairs for stricter isolation; any EnvPairs failure
// then falls back to the trimmed base instead of the host env.
func subprocessEnv(ctx context.Context, workDir, command string, commands map[string][]string, store credentials.Store, cfg ShellBashConfig) []string {
	trim := cfg.TrimEnv != nil && *cfg.TrimEnv
	var base []string
	if trim {
		base = baseProcessExecEnv(workDir)
	} else {
		base = os.Environ()
	}
	base = mergeEnvMap(base, cfg.ExtraEnv)
	resolver, ok := store.(credentials.EnvPairResolver)
	if store == nil || !ok {
		return base
	}
	cmdName, scope := shellScopeForCommand(command)
	if scope == "" {
		return base
	}
	opts := credentials.EnvPairsOptions{}
	if cmdName != "" {
		if keys, ok := commands[cmdName]; ok && len(keys) > 0 {
			opts.Keys = keys
		}
	}
	pairs, err := resolver.EnvPairs(ctx, scope, opts)
	if err != nil {
		slog.Warn("tool/shell-bash: EnvPairs failed; scoped injection skipped", "scope", scope, "err", err, "trimEnv", trim)
		return base
	}
	return mergeEnvEntries(base, pairs)
}

// mergeEnvEntries overlays KEY=value entries onto base, replacing any prior
// entry with the same key (required when base is os.Environ() and scoped
// injection must override a stale host value).
func mergeEnvEntries(base []string, entries []string) []string {
	if len(entries) == 0 {
		return base
	}
	extra := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		extra[key] = value
	}
	return mergeEnvMap(base, extra)
}

// mergeEnvMap appends static KEY=value entries, replacing any base entry with
// the same key.
func mergeEnvMap(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := base
	for _, key := range keys {
		prefix := key + "="
		filtered := out[:0]
		for _, entry := range out {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		out = append(filtered, prefix+extra[key])
	}
	return out
}

func shellScopeForCommand(command string) (cmdName, scope string) {
	cmd := strings.TrimSpace(firstShellCommandToken(command))
	if cmd == "" {
		return "", ""
	}
	cmd = filepath.Base(cmd)
	if cmd == "" || cmd == "." || cmd == ".." {
		return "", ""
	}
	return cmd, "shell-bash." + cmd
}

func firstShellCommandToken(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if command[0] == '\'' || command[0] == '"' {
		quote := command[0]
		if end := strings.IndexByte(command[1:], quote); end >= 0 {
			return command[1 : 1+end]
		}
	}
	if i := strings.IndexAny(command, " \t\n"); i >= 0 {
		return command[:i]
	}
	return command
}

var baseProcessEnvKeys = map[string]struct{}{
	"PATH": {}, "HOME": {}, "USER": {}, "LOGNAME": {}, "SHELL": {},
	"LANG": {}, "LC_ALL": {}, "LC_CTYPE": {}, "TMPDIR": {}, "TZ": {}, "TERM": {},
	"XDG_CACHE_HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_DATA_HOME": {},
}

func baseProcessExecEnv(workDir string) []string {
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
