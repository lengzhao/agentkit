package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

var (
	testServerOnce sync.Once
	testServerBin  string
	testServerErr  error
)

func TestMain(m *testing.M) {
	testServerOnce.Do(func() {
		dir, err := os.MkdirTemp("", "agentkit-mcptestserver-*")
		if err != nil {
			testServerErr = err
			return
		}
		testServerBin = filepath.Join(dir, "mcptestserver")
		if runtime.GOOS == "windows" {
			testServerBin += ".exe"
		}
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			testServerErr = fmt.Errorf("runtime.Caller failed")
			return
		}
		src := filepath.Join(filepath.Dir(file), "testdata", "mcptestserver")
		cmd := exec.Command("go", "build", "-o", testServerBin, ".")
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			testServerErr = fmt.Errorf("build mcptestserver: %w\n%s", err, out)
		}
	})
	if testServerErr != nil {
		fmt.Fprintf(os.Stderr, "mcptestserver build failed: %v\n", testServerErr)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func requireTestMCPServer(t *testing.T) string {
	t.Helper()
	if testServerBin == "" {
		t.Fatal("mcptestserver binary not built")
	}
	return testServerBin
}

func newTestMCPProvider(t *testing.T) (*mcpProvider, string) {
	t.Helper()

	bin := requireTestMCPServer(t)
	dir := t.TempDir()
	mcpRoot := filepath.Join(dir, "mcp-root")
	if err := os.MkdirAll(mcpRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(dir, "mcp.json")
	raw, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"filesystem": map[string]any{
				"command": bin,
				"args":    []string{mcpRoot},
				"prefix":  "fs__",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	return &mcpProvider{
		files:     []string{configPath},
		workspace: &testWorkspace{root: dir},
		pool:      newClientPool(0),
	}, mcpRoot
}

func findToolBySuffix(tools []agentkit.Tool, suffix string) agentkit.Tool {
	for _, tool := range tools {
		if strings.HasSuffix(tool.Name(), suffix) {
			return tool
		}
	}
	return nil
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMCPServerListAndReadWrite(t *testing.T) {
	provider, mcpRoot := newTestMCPProvider(t)
	ctx := context.Background()

	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(tools))
	}

	writeTool := findToolBySuffix(tools, "write_file")
	readTool := findToolBySuffix(tools, "read_text_file")
	if writeTool == nil || readTool == nil {
		var names []string
		for _, tool := range tools {
			names = append(names, tool.Name())
		}
		t.Fatalf("missing tools, got: %s", strings.Join(names, ", "))
	}

	relPath := "hello.txt"
	agenttest.CallTool(t, ctx, writeTool, mustJSON(t, map[string]any{
		"path":    relPath,
		"content": "hello from mcp test",
	}))

	written := filepath.Join(mcpRoot, relPath)
	raw, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("read written file from disk: %v", err)
	}
	if string(raw) != "hello from mcp test" {
		t.Fatalf("file content = %q", string(raw))
	}

	readOut := agenttest.CallTool(t, ctx, readTool, mustJSON(t, map[string]any{
		"path": relPath,
	}))
	if !strings.Contains(readOut, "hello from mcp test") {
		t.Fatalf("read tool output = %q, want file contents", readOut)
	}
}

func TestMCPServerReloadCommand(t *testing.T) {
	provider, _ := newTestMCPProvider(t)
	ctx := context.Background()

	cp, ok := agentkit.ToolProvider(provider).(agentkit.CommandProvider)
	if !ok {
		t.Fatal("provider does not implement CommandProvider")
	}
	cmd := cp.Commands()[0]

	status, err := cmd.CommandExec(ctx, "")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(status, "filesystem") {
		t.Fatalf("status = %q, want filesystem server listed", status)
	}

	reloaded, err := cmd.CommandExec(ctx, "-u")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.Contains(reloaded, "tool(s)") {
		t.Fatalf("reload output = %q", reloaded)
	}
}

func TestMCPServerAddCommand(t *testing.T) {
	bin := requireTestMCPServer(t)
	dir := t.TempDir()
	mcpRoot := filepath.Join(dir, "allowed")
	if err := os.MkdirAll(mcpRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	provider := &mcpProvider{
		files:       []string{filepath.Join(dir, "mcp.json")},
		enableLocal: true,
		workspace:   &testWorkspace{root: dir},
		pool:        newClientPool(0),
	}
	ctx := context.Background()
	cp, ok := agentkit.ToolProvider(provider).(agentkit.CommandProvider)
	if !ok {
		t.Fatal("provider does not implement CommandProvider")
	}
	cmd := cp.Commands()[0]

	serverJSON := mustJSON(t, map[string]any{
		"command": bin,
		"args":    []string{mcpRoot},
		"prefix":  "fs__",
	})
	out, err := cmd.CommandExec(ctx, "add fs "+serverJSON)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.Contains(out, "verified") {
		t.Fatalf("add output = %q", out)
	}

	tools, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(tools))
	}
}
