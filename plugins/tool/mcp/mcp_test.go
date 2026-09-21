package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/filesystem"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/cap/workspace"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	"github.com/lengzhao/agentkit/runtime/telemetry"
)

// testFSForWorkspace builds an unrestricted filesystem/local delegating scoped
// paths to ws (test workspaces resolve to absolute temp paths).
func testFSForWorkspace(t *testing.T, ws workspace.Service) filesystem.Service {
	t.Helper()
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: ".", Unrestricted: true}, rtfilesystem.Deps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

type hintCredentials struct{}

func (hintCredentials) Resolve(_ context.Context, scope string, ref string) (credentials.Secret, error) {
	key := strings.TrimPrefix(ref, "env:")
	return credentials.Secret{}, fmt.Errorf("credential %q is not set for scope %q (/env add %s %s=<value>)", key, scope, scope, key)
}

// countingFS wraps filesystem.Service to count Read calls, so tests can
// tell whether a config file was actually re-read from disk or served from
// the in-memory cache.
type countingFS struct {
	filesystem.Service
	count int
}

func (f *countingFS) Read(ctx context.Context, path string) ([]byte, error) {
	f.count++
	return f.Service.Read(ctx, path)
}

// TestMCPProviderCachingAndSyncCommand verifies that ListTools does not
// re-read mcp.json (nor re-query servers) after the first load, and that the
// "mcp" command forces a reload picking up on-disk changes.
func TestMCPProviderCachingAndSyncCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(configPath, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := &countingFS{Service: testFSForWorkspace(t, &testWorkspace{root: dir})}
	provider := &mcpProvider{
		files: []string{configPath},
		fs:    fs,
		pool:  newClientPool(0),
	}
	ctx := context.Background()

	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %d, want 0", len(tools))
	}
	if fs.count != 1 {
		t.Fatalf("resolve count after first list = %d, want 1", fs.count)
	}

	// Change the file on disk; ListTools should keep serving the cached result
	// (and must not re-read the file).
	flaky := `{"mcpServers":{"flaky":{"command":"this-binary-does-not-exist-xyz123"}}}`
	if err := os.WriteFile(configPath, []byte(flaky), 0o644); err != nil {
		t.Fatal(err)
	}
	tools, err = provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list after edit: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools after edit (pre-sync) = %d, want cache to still report 0", len(tools))
	}
	if fs.count != 1 {
		t.Fatalf("resolve count after cached list = %d, want still 1", fs.count)
	}

	cp, ok := agentkit.ToolProvider(provider).(agentkit.CommandProvider)
	if !ok {
		t.Fatalf("provider does not implement agentkit.CommandProvider: %T", provider)
	}
	cmds := cp.Commands()
	if len(cmds) != 1 || cmds[0].Name() != "mcp" {
		t.Fatalf("commands = %v, want a single \"mcp\" command", cmds)
	}
	if _, err := cmds[0].CommandExec(ctx, "-u"); err != nil {
		t.Fatalf("sync command: %v", err)
	}
	if fs.count != 2 {
		t.Fatalf("resolve count after sync = %d, want 2 (file re-read)", fs.count)
	}

	// The flaky server's command doesn't exist, so it contributes no tools,
	// but the reload must still have happened (proven by the resolve count).
	tools, err = provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list after sync: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools after sync = %d, want 0 (flaky server unreachable)", len(tools))
	}
	if fs.count != 2 {
		t.Fatalf("resolve count after post-sync list = %d, want still 2 (cached again)", fs.count)
	}
}

func TestMCPAddCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	provider := &mcpProvider{
		files:       []string{filepath.Join(dir, "mcp.json")},
		enableLocal: true,
		fs:          testFSForWorkspace(t, &testWorkspace{root: dir}),
		pool:        newClientPool(0),
	}
	ctx := context.Background()
	cp, ok := agentkit.ToolProvider(provider).(agentkit.CommandProvider)
	if !ok {
		t.Fatal("provider does not implement CommandProvider")
	}
	cmd := cp.Commands()[0]

	out, err := cmd.CommandExec(ctx, `add demo {"command":"this-binary-does-not-exist-xyz123"}`)
	if err == nil {
		t.Fatalf("add command should fail for unreachable server, got %q", out)
	}
	if !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("error=%v, want probe failure", err)
	}

	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %d, want 0 after rejected add", len(tools))
	}

	status, err := cmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatalf("status command: %v", err)
	}
	if !strings.Contains(status, "Usage:") {
		t.Fatalf("status=%q, want usage help", status)
	}
}

func TestMCPAddKeepsConfigWhenCredentialsMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	provider := &mcpProvider{
		files:       []string{configPath},
		enableLocal: true,
		fs:          testFSForWorkspace(t, &testWorkspace{root: dir}),
		pool:        newClientPool(0),
		credentials: hintCredentials{},
	}
	ctx := context.Background()
	cmd := agentkit.ToolProvider(provider).(agentkit.CommandProvider).Commands()[0]

	_, err := cmd.CommandExec(ctx, `add knowledge {"transport":"streamable-http","url":"env:KNOWLEDGE_MCP_URL"}`)
	if err == nil {
		t.Fatal("expected probe failure")
	}
	if !strings.Contains(err.Error(), "/env add mcp.knowledge KNOWLEDGE_MCP_URL=") {
		t.Fatalf("error=%v, want /env add hint", err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"knowledge"`) {
		t.Fatalf("config should be kept on missing credentials: %s", raw)
	}
}

func TestMCPAddRequiresGlobalWhenLocalDisabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	provider := &mcpProvider{
		files: []string{"global:mcp.json"},
		fs:    testFSForWorkspace(t, &testWorkspace{root: dir}),
		pool:  newClientPool(0),
	}
	ctx := context.Background()
	cmd := agentkit.ToolProvider(provider).(agentkit.CommandProvider).Commands()[0]

	_, err := cmd.CommandExec(ctx, `add demo {"command":"missing-binary"}`)
	if err == nil || !strings.Contains(err.Error(), "local mcp is disabled") {
		t.Fatalf("add without -g = %v, want local disabled error", err)
	}

	_, err = cmd.CommandExec(ctx, `add -g demo {"command":"missing-binary"}`)
	if err == nil || !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("add with -g = %v, want probe failure after global target selected", err)
	}
}

func TestMCPReloadRecordsInitObservation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(configPath, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &telemetry.RecordingExporter{}
	ctx := telemetry.WithExporter(context.Background(), rec)
	ctx, _ = rec.BeginTurn(ctx, captelemetry.TurnMeta{TurnID: "turn-1"})

	provider := &mcpProvider{
		files: []string{configPath},
		fs:    testFSForWorkspace(t, &testWorkspace{root: dir}),
		pool:  newClientPool(0),
	}
	if _, err := provider.ListTools(ctx); err != nil {
		t.Fatalf("list: %v", err)
	}

	_, observations, _ := rec.Snapshot()
	if len(observations) != 0 {
		t.Fatalf("observations = %d, want 0 when no servers configured", len(observations))
	}
}
