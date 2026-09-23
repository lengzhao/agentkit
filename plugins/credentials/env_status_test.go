package credentials

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestEnvStatusListsScopedKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc.json")
	const secretsPass = "agentkit-test-secrets-passphrase"
	manifestPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(manifestPath, []byte(`{"mcpServers":{"tool":{"command":"echo","env":{"K":"env:AGENTKIT_TEST_SECRET"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewIntegrations(Config{
		EncryptedFile: path,
		ManifestFiles: []string{manifestPath},
		Env: map[string]string{
			SecretsMasterKeyEnv: secretsPass,
		},
	}, EnvDeps{FS: testEnvFS(t)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cmd := store.(agentkit.CommandProvider).Commands()[0]
	if _, err := cmd.CommandExec(ctx, "add mcp.tool AGENTKIT_TEST_SECRET=value"); err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "loaded encrypted keys:") || !strings.Contains(out, "mcp.tool::AGENTKIT_TEST_SECRET") {
		t.Fatalf("output=%q, want loaded encrypted key listed", out)
	}
	if !strings.Contains(out, "scoped env keys:") || !strings.Contains(out, "AGENTKIT_TEST_SECRET") || !strings.Contains(out, "loaded") {
		t.Fatalf("output=%q, want scoped env key inventory", out)
	}
}
