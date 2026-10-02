package shellbwrap

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lengzhao/agentkit/cap/credentials"
)

// shellSubprocessEnv 构造子进程 env。sandboxed 时用白名单基础 env（不继承 Runner 全量
// 进程 env）；命中 credentials scopedEnv（shell-bash.<cmd>）时注入 scoped pairs。
func shellSubprocessEnv(ctx context.Context, workDir, command string, commands map[string][]string, store credentials.Store, sandboxed bool) []string {
	defaultEnv := func() []string {
		if sandboxed {
			return baseProcessExecEnv(workDir)
		}
		return os.Environ()
	}

	if store == nil {
		return defaultEnv()
	}
	resolver, ok := store.(credentials.EnvPairResolver)
	if !ok {
		return defaultEnv()
	}
	cmdName, scope := shellScopeForCommand(command)
	if scope == "" {
		return defaultEnv()
	}
	opts := credentials.EnvPairsOptions{}
	if cmdName != "" {
		if keys, ok := commands[cmdName]; ok && len(keys) > 0 {
			opts.Keys = keys
		}
	}
	base := baseProcessExecEnv(workDir)
	pairs, err := resolver.EnvPairs(ctx, scope, opts)
	if err != nil {
		slog.Debug("shell-bwrap: EnvPairs failed", "scope", scope, "err", err)
		return defaultEnv()
	}
	if len(pairs) == 0 {
		return defaultEnv()
	}
	return append(base, pairs...)
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
	"NO_PROXY": {}, "HTTP_PROXY": {}, "HTTPS_PROXY": {}, "ALL_PROXY": {},
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
