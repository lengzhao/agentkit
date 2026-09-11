package credentials

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtcredentials "github.com/lengzhao/agentkit/runtime/credentials"
)

func TestResolvePrefersContextOverEnvironment(t *testing.T) {
	t.Setenv("AGENTKIT_TEST_SECRET", "from-env")
	store, err := New(Config{}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}

	ctx := rtcredentials.WithSecrets(context.Background(), map[string]string{
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
	store, err := New(Config{}, EnvDeps{})
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

func TestResolveFallsBackToConfigEnv(t *testing.T) {
	store, err := New(Config{Env: map[string]string{
		"AGENTKIT_TEST_SECRET": "from-config",
	}}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}

	secret, err := store.Resolve(context.Background(), "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "from-config" {
		t.Fatalf("value=%q, want from-config", secret.Value)
	}
}

func TestResolveConfigEnvUsesPrefix(t *testing.T) {
	store, err := New(Config{
		Prefix: "PREFIX_",
		Env: map[string]string{
			"AGENTKIT_TEST_SECRET": "from-config",
		},
	}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}

	secret, err := store.Resolve(context.Background(), "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "from-config" {
		t.Fatalf("value=%q, want from-config", secret.Value)
	}
}

func TestResolveEnvironmentOverridesConfigEnv(t *testing.T) {
	t.Setenv("AGENTKIT_TEST_SECRET", "from-env")
	store, err := New(Config{Env: map[string]string{
		"AGENTKIT_TEST_SECRET": "from-config",
	}}, EnvDeps{})
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

func TestResolveConfigEnvOverridesEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("AGENTKIT_TEST_SECRET=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New(Config{
		EncryptedFile: EncryptedFileDisabled,
		Files:         []string{path},
		Env: map[string]string{
			"AGENTKIT_TEST_SECRET": "from-config",
		},
	}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}

	secret, err := store.Resolve(context.Background(), "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "from-config" {
		t.Fatalf("value=%q, want from-config", secret.Value)
	}
}

func TestResolveFallsBackToEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("AGENTKIT_TEST_SECRET=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New(Config{EncryptedFile: EncryptedFileDisabled, Files: []string{path}}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}

	secret, err := store.Resolve(context.Background(), "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "from-file" {
		t.Fatalf("value=%q, want from-file", secret.Value)
	}
}

func TestResolveEnvironmentOverridesEnvFile(t *testing.T) {
	t.Setenv("AGENTKIT_TEST_SECRET", "from-env")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("AGENTKIT_TEST_SECRET=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New(Config{EncryptedFile: EncryptedFileDisabled, Files: []string{path}}, EnvDeps{})
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
	store, err := New(Config{Prefix: "PREFIX_"}, EnvDeps{})
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

	store, err := New(Config{}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Resolve(context.Background(), "env:DOES_NOT_EXIST_"+os.Getenv("USER"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnvReloadCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("AGENTKIT_TEST_SECRET=before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New(Config{EncryptedFile: EncryptedFileDisabled, Files: []string{path}}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	secret, err := store.Resolve(ctx, "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "before" {
		t.Fatalf("value=%q, want before", secret.Value)
	}

	if err := os.WriteFile(path, []byte("AGENTKIT_TEST_SECRET=after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret, err = store.Resolve(ctx, "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "before" {
		t.Fatalf("value before reload=%q, want cached before", secret.Value)
	}

	cp, ok := store.(agentkit.CommandProvider)
	if !ok {
		t.Fatalf("store does not implement CommandProvider: %T", store)
	}
	cmds := cp.Commands()
	if len(cmds) != 1 || cmds[0].Name() != "env" {
		t.Fatalf("commands = %v, want a single \"env\" command", cmds)
	}
	if _, err := cmds[0].CommandExec(ctx, "-u"); err != nil {
		t.Fatalf("sync command: %v", err)
	}

	secret, err = store.Resolve(ctx, "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "after" {
		t.Fatalf("value after reload=%q, want after", secret.Value)
	}
}

func TestEnvCommandSanitizeArgsForLog(t *testing.T) {
	t.Parallel()
	cmd := &envSyncCommand{}
	got := cmd.SanitizeArgsForLog("add FOO=bar BAZ=secret")
	want := "add FOO=" + agentkit.SlashLogRedacted + " BAZ=" + agentkit.SlashLogRedacted
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if cmd.SanitizeArgsForLog("-u") != "-u" {
		t.Fatal("expected -u unchanged")
	}
	if redactEnvAddArgsForLog("") != "" {
		t.Fatal("expected empty unchanged")
	}
}

func TestEnvAddCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	store, err := New(Config{EncryptedFile: EncryptedFileDisabled, Files: []string{path}}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cp := store.(agentkit.CommandProvider)
	cmd := cp.Commands()[0]

	out, err := cmd.CommandExec(ctx, "add AGENTKIT_TEST_SECRET=injected")
	if err != nil {
		t.Fatalf("add command: %v", err)
	}
	if !strings.Contains(out, "verified") {
		t.Fatalf("output=%q, want verified", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "AGENTKIT_TEST_SECRET=injected") {
		t.Fatalf(".env=%q, want injected key", data)
	}
	secret, err := store.Resolve(ctx, "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "injected" {
		t.Fatalf("value=%q, want injected", secret.Value)
	}

	status, err := cmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatalf("status command: %v", err)
	}
	if !strings.Contains(status, "Usage:") {
		t.Fatalf("status=%q, want usage help", status)
	}
}

func TestEnvAddRejectsOnVerifyFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	store, err := New(Config{EncryptedFile: EncryptedFileDisabled, Files: []string{path}}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cp := store.(agentkit.CommandProvider)
	cmd := cp.Commands()[0]

	_, err = cmd.CommandExec(ctx, "add AGENTKIT_EMPTY_VERIFY_TEST=")
	if err == nil {
		t.Fatal("expected error for empty value")
	}

	_, err = store.Resolve(ctx, "env:AGENTKIT_EMPTY_VERIFY_TEST")
	if err == nil {
		t.Fatal("expected key to be rolled back")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), "AGENTKIT_EMPTY_VERIFY_TEST") {
			t.Fatalf(".env should not contain rejected key: %s", data)
		}
	}
}

func TestEnvAddEncryptedCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc.json")
	const secretsPass = "agentkit-test-secrets-passphrase"
	store, err := New(Config{
		EncryptedFile: path,
		Env: map[string]string{
			rtcredentials.SecretsMasterKeyEnv: secretsPass,
		},
	}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cp := store.(agentkit.CommandProvider)
	cmd := cp.Commands()[0]

	out, err := cmd.CommandExec(ctx, "add AGENTKIT_TEST_SECRET=injected")
	if err != nil {
		t.Fatalf("add command: %v", err)
	}
	if !strings.Contains(out, "verified") {
		t.Fatalf("output=%q, want verified", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "injected") {
		t.Fatalf("secrets file must not contain plaintext: %s", data)
	}
	secret, err := store.Resolve(ctx, "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "injected" {
		t.Fatalf("value=%q, want injected", secret.Value)
	}
}

func TestResolveEncryptedOverridesDotenv(t *testing.T) {
	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	secretsPath := filepath.Join(dir, "secrets.enc.json")
	if err := os.WriteFile(dotenv, []byte("AGENTKIT_TEST_SECRET=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const secretsPass = "agentkit-test-secrets-passphrase"
	key, err := rtcredentials.ParseSecretsMasterKey(secretsPass)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := rtcredentials.EncryptSecretsFile(map[string]string{
		"AGENTKIT_TEST_SECRET": "from-encrypted",
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretsPath, enc, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := New(Config{
		EncryptedFile: secretsPath,
		Files:         []string{dotenv},
		Env: map[string]string{
			rtcredentials.SecretsMasterKeyEnv: secretsPass,
		},
	}, EnvDeps{})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := store.Resolve(context.Background(), "env:AGENTKIT_TEST_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != "from-encrypted" {
		t.Fatalf("value=%q, want from-encrypted", secret.Value)
	}
}
