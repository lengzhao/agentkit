// Package mcptest builds the stdio MCP test server and returns a tool/mcp provider for smoke tests.
package mcptest

import (
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
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	mcpplugin "github.com/lengzhao/agentkit/plugins/tool/mcp"
)

var (
	serverOnce sync.Once
	serverBin  string
	serverErr  error
)

func ensureServerBuilt() error {
	serverOnce.Do(func() {
		dir, err := os.MkdirTemp("", "agentkit-mcptestserver-*")
		if err != nil {
			serverErr = err
			return
		}
		serverBin = filepath.Join(dir, "mcptestserver")
		if runtime.GOOS == "windows" {
			serverBin += ".exe"
		}
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			serverErr = fmt.Errorf("runtime.Caller failed")
			return
		}
		src := filepath.Join(filepath.Dir(file), "..", "..", "plugins", "tool", "mcp", "testdata", "mcptestserver")
		cmd := exec.Command("go", "build", "-o", serverBin, ".")
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			serverErr = fmt.Errorf("build mcptestserver: %w\n%s", err, out)
		}
	})
	return serverErr
}

// ServerBinaryAndRoot returns the built test MCP server binary and a fresh allowed root directory.
func ServerBinaryAndRoot(t *testing.T) (bin string, root string) {
	t.Helper()
	if err := ensureServerBuilt(); err != nil {
		t.Fatal(err)
	}
	if serverBin == "" {
		t.Fatal("mcptestserver binary not built")
	}
	root = filepath.Join(t.TempDir(), "mcp-root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return serverBin, root
}

// NewProvider wires tool/mcp against a temp mcp.json that launches the test stdio server.
// The returned root is the MCP server's allowed filesystem root.
func NewProvider(t *testing.T) (agentkit.ToolProvider, string) {
	t.Helper()
	bin, mcpRoot := ServerBinaryAndRoot(t)

	dir := t.TempDir()
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

	provider, err := mcpplugin.NewMCP(mcpplugin.MCPConfig{
		EnableLocal: true,
		Files:       []string{configPath},
	}, mcpplugin.MCPDeps{
		Workspace: rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider, mcpRoot
}

// ToolBySuffix returns the first tool whose name ends with suffix.
func ToolBySuffix(t *testing.T, tools []agentkit.Tool, suffix string) agentkit.Tool {
	t.Helper()
	for _, tool := range tools {
		if strings.HasSuffix(tool.Name(), suffix) {
			return tool
		}
	}
	t.Fatalf("no tool with suffix %q among %d tools", suffix, len(tools))
	return nil
}
