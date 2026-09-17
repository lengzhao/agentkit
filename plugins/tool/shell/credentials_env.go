package shell

import (
	"context"
	"log/slog"
	"os"

	"github.com/lengzhao/agentkit/cap/credentials"
	rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"
)

func subprocessEnv(ctx context.Context, workDir, command string, commands map[string][]string, store credentials.Store) []string {
	if store == nil {
		return os.Environ()
	}
	resolver, ok := store.(credentials.EnvPairResolver)
	if !ok {
		return os.Environ()
	}
	cmdName, scope := shellScopeForCommand(command)
	if scope == "" {
		return os.Environ()
	}
	opts := credentials.EnvPairsOptions{}
	if cmdName != "" {
		if keys, ok := commands[cmdName]; ok && len(keys) > 0 {
			opts.Keys = keys
		}
	}
	base := rtcredentials.BaseProcessExecEnv(workDir)
	pairs, err := resolver.EnvPairs(ctx, scope, opts)
	if err != nil {
		slog.Debug("shell-bash: EnvPairs failed", "scope", scope, "err", err)
		return os.Environ()
	}
	if len(pairs) == 0 {
		// No scoped secrets for this command: keep full host env (e.g. go/make). Trim only when injecting.
		return os.Environ()
	}
	return append(base, pairs...)
}

func shellScopeForCommand(command string) (cmdName, scope string) {
	return rtcredentials.ShellBashScopeForCommand(command)
}
