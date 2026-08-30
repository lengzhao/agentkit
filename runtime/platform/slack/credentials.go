package slack

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit/cap/credentials"
)

func resolveToken(ctx context.Context, inline, ref string, store credentials.Store, field string) (string, error) {
	if inline != "" {
		return inline, nil
	}
	if ref == "" {
		return "", nil
	}
	if store == nil {
		return "", fmt.Errorf("platform/slack %s requires credentials dependency", field)
	}
	secret, err := store.Resolve(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("platform/slack resolve %s %q: %w", field, ref, err)
	}
	if secret.Value == "" {
		return "", fmt.Errorf("platform/slack %s %q is empty", field, ref)
	}
	return secret.Value, nil
}
