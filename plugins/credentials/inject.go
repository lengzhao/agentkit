package credentials

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit/cap/credentials"
)

func injectScopedEnv(ctx context.Context, base []string, scope string, keys []string, store credentials.Store) []string {
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
