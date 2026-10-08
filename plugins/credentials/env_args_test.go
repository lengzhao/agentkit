package credentials

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestEnvCommandRejectsUpdateWithAdd(t *testing.T) {
	dir := t.TempDir()
	store, err := NewIntegrations(Config{
		SecretsKey:    "config-passphrase",
		EncryptedFile: filepath.Join(dir, "secrets.enc.json"),
	}, EnvDeps{FS: testEnvFS(t)})
	if err != nil {
		t.Fatal(err)
	}
	cmd := store.(agentkit.CommandProvider).Commands()[0]
	_, err = cmd.CommandExec(context.Background(), "-u add mcp.tool FOO=bar")
	if err == nil {
		t.Fatal("expected error for -u add")
	}
}

func TestParseEnvSlashArgsValueWithSpaces(t *testing.T) {
	t.Parallel()
	_, sub, scope, pair, err := parseEnvSlashArgs("add mcp.n8n-wwa N8N_WWA_TOKEN=Bearer eyJhbGciOiJIUzI1NiJ9")
	if err != nil {
		t.Fatal(err)
	}
	if sub != "add" || scope != "mcp.n8n-wwa" {
		t.Fatalf("sub=%q scope=%q", sub, scope)
	}
	want := "N8N_WWA_TOKEN=Bearer eyJhbGciOiJIUzI1NiJ9"
	if pair != want {
		t.Fatalf("pair=%q want %q", pair, want)
	}
}

func TestEnvAddPreservesSpacesInValue(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(manifestPath, []byte(`{"mcpServers":{"n8n-wwa":{"env":{"N8N_WWA_TOKEN":"env:N8N_WWA_TOKEN"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewIntegrations(Config{
		SecretsKey:    "config-passphrase",
		EncryptedFile: filepath.Join(dir, "secrets.enc.json"),
		ManifestFiles: []string{manifestPath},
	}, EnvDeps{FS: testEnvFS(t)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cmd := store.(agentkit.CommandProvider).Commands()[0]
	want := "Bearer eyJhbGciOiJIUzI1NiJ9"
	if _, err := cmd.CommandExec(ctx, "add mcp.n8n-wwa N8N_WWA_TOKEN="+want); err != nil {
		t.Fatalf("env add: %v", err)
	}
	secret, err := store.Resolve(ctx, "mcp.n8n-wwa", "env:N8N_WWA_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Value != want {
		t.Fatalf("value=%q want %q", secret.Value, want)
	}
}
