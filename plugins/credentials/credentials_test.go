package credentials

import (
	"context"
	"os"
	"testing"

	"github.com/lengzhao/agentkit/cap/credentials"
)

func TestResolvePrefersContextOverEnvironment(t *testing.T) {
	t.Setenv("AGENTKIT_TEST_SECRET", "from-env")
	store, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}

	ctx := credentials.WithSecrets(context.Background(), map[string]string{
		"AGENTKIT_TEST_SECRET": "from-ctx",
	})
	secret, err := store.Resolve(ctx, "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "from-ctx" {
		t.Fatalf("value=%q, want from-ctx", secret.Value)
	}
}

func TestResolveFallsBackToEnvironment(t *testing.T) {
	t.Setenv("AGENTKIT_TEST_SECRET", "from-env")
	store, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}

	secret, err := store.Resolve(context.Background(), "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "from-env" {
		t.Fatalf("value=%q, want from-env", secret.Value)
	}
}

func TestResolveUsesPrefixAfterContextMiss(t *testing.T) {
	t.Setenv("PREFIX_AGENTKIT_TEST_SECRET", "prefixed")
	store, err := New(Config{Prefix: "PREFIX_"})
	if err != nil {
		t.Fatal(err)
	}

	secret, err := store.Resolve(context.Background(), "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "prefixed" {
		t.Fatalf("value=%q, want prefixed", secret.Value)
	}
}

func TestResolveMissing(t *testing.T) {
	t.Parallel()

	store, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Resolve(context.Background(), "env:DOES_NOT_EXIST_"+os.Getenv("USER"))
	if err == nil {
		t.Fatal("expected error")
	}
}
