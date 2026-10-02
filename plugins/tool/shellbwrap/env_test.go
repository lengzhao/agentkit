package shellbwrap

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type plainCredentialStore struct{}

func (plainCredentialStore) Resolve(context.Context, string, string) (credentials.Secret, error) {
	return credentials.Secret{}, nil
}

func hasEnvEntry(env []string, entry string) bool {
	for _, e := range env {
		if e == entry {
			return true
		}
	}
	return false
}

func hasEnvPrefix(env []string, prefix string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func TestShellSubprocessEnvSandboxedStripsRunnerSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-leak")
	t.Setenv("AGENTKIT_SHELL_HOST_MARKER", "1")
	t.Setenv("PATH", "/usr/bin:/bin")

	env := shellSubprocessEnv(context.Background(), "/work", "echo hi", nil, nil, true)
	if hasEnvPrefix(env, "OPENAI_API_KEY=") {
		t.Fatalf("runner secret leaked into sandboxed env: %v", env)
	}
	if hasEnvPrefix(env, "AGENTKIT_SHELL_HOST_MARKER=") {
		t.Fatalf("non-allowlisted host env leaked: %v", env)
	}
	if !hasEnvEntry(env, "PWD=/work") {
		t.Fatalf("missing PWD in trimmed base: %v", env)
	}
}

func TestShellSubprocessEnvUnsandboxedInheritsHostEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-visible")
	env := shellSubprocessEnv(context.Background(), "/tmp", "echo hi", nil, nil, false)
	if !hasEnvPrefix(env, "OPENAI_API_KEY=") {
		t.Fatal("unsandboxed mode must inherit host env")
	}
}

func TestShellSubprocessEnvScopedInjectionSandboxed(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-leak")
	env := shellSubprocessEnv(context.Background(), "/work", "gh auth status", nil, agenttest.StubEnvPairResolver{
		Pairs: map[string][]string{"shell-bash.gh": {"GH_TOKEN=gh-scoped"}},
	}, true)
	if !hasEnvEntry(env, "GH_TOKEN=gh-scoped") {
		t.Fatalf("missing scoped injection: %v", env)
	}
	if hasEnvPrefix(env, "OPENAI_API_KEY=") {
		t.Fatal("host secret must not leak alongside scoped injection")
	}
}

func TestShellSubprocessEnvCommandsKeysOverride(t *testing.T) {
	var gotOpts credentials.EnvPairsOptions
	resolver := agenttest.StubEnvPairResolver{
		Pairs: map[string][]string{"shell-bash.gh": {"GH_TOKEN=ignored"}},
	}
	capturing := envPairCapture{inner: resolver, out: &gotOpts}
	commands := map[string][]string{"gh": {"CUSTOM_TOKEN=from-config"}}
	env := shellSubprocessEnv(context.Background(), "/work", "gh pr list", commands, capturing, true)
	if !slices.Equal(gotOpts.Keys, commands["gh"]) {
		t.Fatalf("EnvPairs opts.Keys = %v, want %v", gotOpts.Keys, commands["gh"])
	}
	if !hasEnvEntry(env, "GH_TOKEN=ignored") {
		t.Fatalf("expected resolver pairs in env: %v", env)
	}
}

type envPairCapture struct {
	inner credentials.EnvPairResolver
	out   *credentials.EnvPairsOptions
}

func (e envPairCapture) Resolve(ctx context.Context, scope, ref string) (credentials.Secret, error) {
	if s, ok := e.inner.(credentials.Store); ok {
		return s.Resolve(ctx, scope, ref)
	}
	return credentials.Secret{}, nil
}

func (e envPairCapture) EnvPairs(ctx context.Context, scope string, opts credentials.EnvPairsOptions) ([]string, error) {
	*e.out = opts
	return e.inner.EnvPairs(ctx, scope, opts)
}

func TestShellSubprocessEnvFallbackBranches(t *testing.T) {
	t.Setenv("AGENTKIT_SHELL_HOST_MARKER", "1")
	ctx := context.Background()
	work := "/work"
	resolver := agenttest.StubEnvPairResolver{
		Pairs: map[string][]string{"shell-bash.gh": {"GH_TOKEN=x"}},
	}

	t.Run("nil_store_sandboxed", func(t *testing.T) {
		env := shellSubprocessEnv(ctx, work, "gh auth", nil, nil, true)
		if hasEnvPrefix(env, "AGENTKIT_SHELL_HOST_MARKER=") {
			t.Fatal("nil store must use trimmed base when sandboxed")
		}
	})

	t.Run("store_not_resolver", func(t *testing.T) {
		env := shellSubprocessEnv(ctx, work, "gh auth", nil, plainCredentialStore{}, true)
		if hasEnvPrefix(env, "AGENTKIT_SHELL_HOST_MARKER=") {
			t.Fatal("non-EnvPairResolver store must fall back to trimmed base")
		}
	})

	t.Run("empty_scope", func(t *testing.T) {
		env := shellSubprocessEnv(ctx, work, "", nil, resolver, true)
		if hasEnvPrefix(env, "AGENTKIT_SHELL_HOST_MARKER=") {
			t.Fatal("empty command must use trimmed base")
		}
	})

	t.Run("env_pairs_error", func(t *testing.T) {
		env := shellSubprocessEnv(ctx, work, "gh auth", nil, agenttest.StubEnvPairResolver{Err: errors.New("boom")}, true)
		if hasEnvPrefix(env, "AGENTKIT_SHELL_HOST_MARKER=") {
			t.Fatal("EnvPairs error must fall back to trimmed base when sandboxed")
		}
	})

	t.Run("empty_pairs", func(t *testing.T) {
		env := shellSubprocessEnv(ctx, work, "gh auth", nil, agenttest.StubEnvPairResolver{
			Pairs: map[string][]string{"shell-bash.gh": nil},
		}, true)
		if hasEnvPrefix(env, "AGENTKIT_SHELL_HOST_MARKER=") {
			t.Fatal("empty scoped pairs must fall back to trimmed base")
		}
	})
}

func TestShellScopeForCommandBwrap(t *testing.T) {
	t.Parallel()
	cmd, scope := shellScopeForCommand("gh auth status")
	if cmd != "gh" || scope != "shell-bash.gh" {
		t.Fatalf("got cmd=%q scope=%q", cmd, scope)
	}
}
