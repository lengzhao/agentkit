package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/credentials"
)

type stubEnvPairResolver struct {
	pairs map[string][]string
	err   error
}

func (s stubEnvPairResolver) Resolve(context.Context, string, string) (credentials.Secret, error) {
	return credentials.Secret{}, nil
}

func (s stubEnvPairResolver) EnvPairs(_ context.Context, scope string, _ credentials.EnvPairsOptions) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.pairs[scope], nil
}

func TestSubprocessEnvNoInjectionUsesHostEnviron(t *testing.T) {
	host := "AGENTKIT_SHELL_HOST_MARKER=1"
	t.Setenv("AGENTKIT_SHELL_HOST_MARKER", "1")
	env := subprocessEnv(context.Background(), "/tmp", "go test ./...", nil, stubEnvPairResolver{
		pairs: map[string][]string{"shell-bash.go": nil},
	})
	found := false
	for _, e := range env {
		if e == host {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected host env when no scoped pairs; got %d vars", len(env))
	}
}

func TestSubprocessEnvInjectionUsesTrimmedBase(t *testing.T) {
	t.Setenv("AGENTKIT_SHELL_LEAK_ME", "secret")
	env := subprocessEnv(context.Background(), "/tmp", "gh auth status", nil, stubEnvPairResolver{
		pairs: map[string][]string{"shell-bash.gh": {"GH_TOKEN=gh-test"}},
	})
	hasToken := false
	hasLeak := false
	for _, e := range env {
		if e == "GH_TOKEN=gh-test" {
			hasToken = true
		}
		if strings.HasPrefix(e, "AGENTKIT_SHELL_LEAK_ME=") {
			hasLeak = true
		}
	}
	if !hasToken {
		t.Fatalf("missing injected pair: %v", env)
	}
	if hasLeak {
		t.Fatal("host secret leaked into subprocess env when scoped injection is active")
	}
}
