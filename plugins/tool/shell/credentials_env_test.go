package shell

import (
	"context"
	"errors"
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

func hasEnvEntry(env []string, entry string) bool {
	for _, e := range env {
		if e == entry {
			return true
		}
	}
	return false
}

func trimEnvCfg() ShellBashConfig {
	trim := true
	return ShellBashConfig{TrimEnv: &trim}
}

func TestSubprocessEnvDefaultInheritsHostEnv(t *testing.T) {
	t.Setenv("AGENTKIT_SHELL_HOST_MARKER", "1")
	env := subprocessEnv(context.Background(), "/tmp", "go test ./...", nil, stubEnvPairResolver{
		pairs: map[string][]string{"shell-bash.go": nil},
	}, ShellBashConfig{})
	if !hasEnvEntry(env, "AGENTKIT_SHELL_HOST_MARKER=1") {
		t.Fatal("default should inherit full host env (trimEnv off)")
	}
}

func TestSubprocessEnvTrimEnvUsesTrimmedBase(t *testing.T) {
	t.Setenv("AGENTKIT_SHELL_HOST_MARKER", "1")
	env := subprocessEnv(context.Background(), "/tmp", "go test ./...", nil, stubEnvPairResolver{
		pairs: map[string][]string{"shell-bash.go": nil},
	}, trimEnvCfg())
	if hasEnvEntry(env, "AGENTKIT_SHELL_HOST_MARKER=1") {
		t.Fatal("host env leaked into subprocess env with trimEnv on")
	}
	if !hasEnvEntry(env, "PWD=/tmp") {
		t.Fatalf("missing trimmed base PWD: %v", env)
	}
}

func TestSubprocessEnvTrimEnvNilStore(t *testing.T) {
	t.Setenv("AGENTKIT_SHELL_HOST_MARKER", "1")
	env := subprocessEnv(context.Background(), "/tmp", "env", nil, nil, trimEnvCfg())
	if hasEnvEntry(env, "AGENTKIT_SHELL_HOST_MARKER=1") {
		t.Fatal("host env leaked into subprocess env without credentials store")
	}
}

func TestSubprocessEnvTrimEnvErrorUsesTrimmedBase(t *testing.T) {
	t.Setenv("AGENTKIT_SHELL_HOST_MARKER", "1")
	env := subprocessEnv(context.Background(), "/tmp", "gh auth status", nil, stubEnvPairResolver{
		err: errors.New("store unavailable"),
	}, trimEnvCfg())
	if hasEnvEntry(env, "AGENTKIT_SHELL_HOST_MARKER=1") {
		t.Fatal("host env leaked into subprocess env on EnvPairs error")
	}
}

func TestSubprocessEnvErrorDefaultKeepsHostEnv(t *testing.T) {
	t.Setenv("AGENTKIT_SHELL_HOST_MARKER", "1")
	env := subprocessEnv(context.Background(), "/tmp", "gh auth status", nil, stubEnvPairResolver{
		err: errors.New("store unavailable"),
	}, ShellBashConfig{})
	if !hasEnvEntry(env, "AGENTKIT_SHELL_HOST_MARKER=1") {
		t.Fatal("EnvPairs error should keep host env when trimEnv off")
	}
}

func TestSubprocessEnvExtraEnv(t *testing.T) {
	env := subprocessEnv(context.Background(), "/tmp", "env", nil, nil, ShellBashConfig{
		ExtraEnv: map[string]string{"CI": "1"},
	})
	if !hasEnvEntry(env, "CI=1") {
		t.Fatalf("missing extraEnv entry: %v", env)
	}
}

func TestSubprocessEnvTrimEnvInjection(t *testing.T) {
	t.Setenv("AGENTKIT_SHELL_LEAK_ME", "secret")
	env := subprocessEnv(context.Background(), "/tmp", "gh auth status", nil, stubEnvPairResolver{
		pairs: map[string][]string{"shell-bash.gh": {"GH_TOKEN=gh-test"}},
	}, trimEnvCfg())
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
		t.Fatal("host secret leaked into subprocess env with trimEnv on")
	}
}

func TestSubprocessEnvDefaultInjectionAppendsToHostEnv(t *testing.T) {
	t.Setenv("AGENTKIT_SHELL_HOST_MARKER", "1")
	env := subprocessEnv(context.Background(), "/tmp", "gh auth status", nil, stubEnvPairResolver{
		pairs: map[string][]string{"shell-bash.gh": {"GH_TOKEN=gh-test"}},
	}, ShellBashConfig{})
	if !hasEnvEntry(env, "GH_TOKEN=gh-test") {
		t.Fatalf("missing injected pair: %v", env)
	}
	if !hasEnvEntry(env, "AGENTKIT_SHELL_HOST_MARKER=1") {
		t.Fatal("default should keep host env alongside scoped injection")
	}
}

func TestSubprocessEnvInjectionOverridesHostKey(t *testing.T) {
	t.Setenv("GH_TOKEN", "from-host")
	env := subprocessEnv(context.Background(), "/tmp", "gh auth status", nil, stubEnvPairResolver{
		pairs: map[string][]string{"shell-bash.gh": {"GH_TOKEN=from-scope"}},
	}, ShellBashConfig{})
	if !hasEnvEntry(env, "GH_TOKEN=from-scope") {
		t.Fatalf("scoped value must override host env: %v", env)
	}
	if hasEnvEntry(env, "GH_TOKEN=from-host") {
		t.Fatal("duplicate GH_TOKEN must not remain after override")
	}
}

func TestFirstShellCommandToken(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"gh pr list":    "gh",
		"  npm install": "npm",
		"'my tool' run": "my tool",
		"echo hello":    "echo",
		"":              "",
	}
	for cmd, want := range cases {
		if got := firstShellCommandToken(cmd); got != want {
			t.Fatalf("firstShellCommandToken(%q)=%q want %q", cmd, got, want)
		}
	}
}

func TestShellScopeForCommand(t *testing.T) {
	t.Parallel()
	cmd, scope := shellScopeForCommand("gh auth status")
	if cmd != "gh" || scope != "shell-bash.gh" {
		t.Fatalf("got cmd=%q scope=%q", cmd, scope)
	}
	cmd, scope = shellScopeForCommand("/usr/bin/gh auth status")
	if cmd != "gh" || scope != "shell-bash.gh" {
		t.Fatalf("path cmd=%q scope=%q", cmd, scope)
	}
}
