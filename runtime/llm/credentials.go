package llm

import (
	"context"
	"fmt"
	"os"

	"github.com/lengzhao/agentkit/cap/credentials"
)

func resolveAPIKey(ctx context.Context, apiKey, apiKeyRef string, store credentials.Store) (string, error) {
	if apiKey != "" {
		return apiKey, nil
	}
	if apiKeyRef != "" {
		if store == nil {
			return "", fmt.Errorf("apiKeyRef %q requires credentials dependency", apiKeyRef)
		}
		secret, err := store.Resolve(ctx, apiKeyRef)
		if err != nil {
			return "", fmt.Errorf("resolve apiKeyRef %q: %w", apiKeyRef, err)
		}
		return secret.Value, nil
	}
	if value := os.Getenv("OPENAI_API_KEY"); value != "" {
		return value, nil
	}
	if value := os.Getenv("OPENAI_COMPATIBLE_API_KEY"); value != "" {
		return value, nil
	}
	return "", nil
}
