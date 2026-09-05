package credentials_test

import (
	"context"
	"testing"

	rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"
)

func TestSecretFromContextMatchesRefAndEnvKey(t *testing.T) {
	t.Parallel()

	ctx := rtcredentials.WithSecrets(context.Background(), map[string]string{
		"OPENAI_API_KEY": "from-ctx",
	})

	secret, ok := rtcredentials.SecretFromContext(ctx, "env:OPENAI_API_KEY")
	if !ok {
		t.Fatal("expected secret in context")
	}
	if secret.Value != "from-ctx" {
		t.Fatalf("value=%q, want from-ctx", secret.Value)
	}
	if secret.Ref != "env:OPENAI_API_KEY" {
		t.Fatalf("ref=%q, want env:OPENAI_API_KEY", secret.Ref)
	}
}

func TestSecretFromContextMissing(t *testing.T) {
	t.Parallel()

	if _, ok := rtcredentials.SecretFromContext(context.Background(), "env:MISSING"); ok {
		t.Fatal("expected no secret")
	}
}
