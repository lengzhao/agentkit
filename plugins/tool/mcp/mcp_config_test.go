package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigFile(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "mcpServers": {
    "github": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "ghcr.io/example/github-mcp"],
      "env": {"GITHUB_TOKEN": "env:GITHUB_TOKEN"},
      "prefix": "gh__",
      "allowTools": ["search"],
      "timeoutSeconds": 30
    },
    "remote": {
      "url": "http://127.0.0.1:8080/mcp",
      "type": "sse"
    }
  }
}`)

	servers, err := parseConfigFile("/tmp/mcp.json", raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("servers = %d, want 2", len(servers))
	}
	var github *serverConfig
	for i := range servers {
		if servers[i].Name == "github" {
			github = &servers[i]
		}
	}
	if github == nil {
		t.Fatal("github server missing")
	}
	if github.Command != "docker" {
		t.Fatalf("command = %q", github.Command)
	}
	if len(github.Args) != 4 {
		t.Fatalf("args = %v", github.Args)
	}
	if github.Env["GITHUB_TOKEN"] != "env:GITHUB_TOKEN" {
		t.Fatalf("env = %v", github.Env)
	}
	if github.toolPrefix() != "gh__" {
		t.Fatalf("prefix = %q", github.toolPrefix())
	}
	if !github.allowsTool("search") {
		t.Fatal("search should be allowed")
	}
	if github.allowsTool("other") {
		t.Fatal("other should be denied by allowTools")
	}
}

func TestParseConfigFileTransportHeaders(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
  "mcpServers": {
    "agenthub": {
      "transport": "streamable-http",
      "url": "env:AGENTHUB_MCP_URL",
      "headers": {
        "X-agenthub-apikey": "env:AGENTHUB_API_KEY"
      }
    }
  }
}`)

	servers, err := parseConfigFile("/tmp/mcp.json", raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(servers))
	}
	server := servers[0]
	if server.Type != "streamable-http" {
		t.Fatalf("type = %q", server.Type)
	}
	if server.URL != "env:AGENTHUB_MCP_URL" {
		t.Fatalf("url = %q", server.URL)
	}
	if server.Headers["X-agenthub-apikey"] != "env:AGENTHUB_API_KEY" {
		t.Fatalf("headers = %v", server.Headers)
	}
}

func TestResolveMCPFiles(t *testing.T) {
	t.Parallel()

	got := resolveMCPFiles(MCPConfig{})
	if len(got) != 1 || got[0] != defaultGlobalMCPFile {
		t.Fatalf("default = %v, want only global", got)
	}

	got = resolveMCPFiles(MCPConfig{EnableLocal: true})
	if len(got) != 2 || got[0] != defaultLocalMCPFile || got[1] != defaultGlobalMCPFile {
		t.Fatalf("enableLocal default = %v", got)
	}

	got = resolveMCPFiles(MCPConfig{
		Files: []string{"local:mcp.json", "global:mcp.json", ".cursor/mcp.json"},
	})
	if len(got) != 1 || got[0] != "global:mcp.json" {
		t.Fatalf("filter local when disabled = %v", got)
	}

	got = resolveMCPFiles(MCPConfig{
		EnableLocal: true,
		Files:     []string{"local:mcp.json", "global:mcp.json"},
	})
	if len(got) != 2 {
		t.Fatalf("explicit with enableLocal = %v", got)
	}
}

func TestLoadServersPrecedence(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	project := filepath.Join(dir, ".cursor", "mcp.json")
	global := filepath.Join(dir, "global-mcp.json")
	if err := os.MkdirAll(filepath.Dir(project), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte(`{"mcpServers":{"shared":{"command":"a"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte(`{"mcpServers":{"shared":{"command":"b"},"extra":{"command":"c"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := &mcpProvider{
		files: []string{".cursor/mcp.json", "global:mcp.json"},
		workspace: &testWorkspace{root: dir},
		pool: newClientPool(0),
	}
	servers, err := provider.loadServers(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("servers = %d, want 2", len(servers))
	}
	byName := map[string]serverConfig{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	if byName["shared"].Command != "a" {
		t.Fatalf("shared command = %q, want project override", byName["shared"].Command)
	}
	if byName["shared"].Global {
		t.Fatal("shared from local file should not be global")
	}
	if byName["extra"].Command != "c" {
		t.Fatalf("extra command = %q", byName["extra"].Command)
	}
	if !byName["extra"].Global {
		t.Fatal("extra from global:mcp.json should be global")
	}
}

type testWorkspace struct {
	root string
}

func (w *testWorkspace) Resolve(_ context.Context, rel string) (string, error) {
	if rel == "global:mcp.json" {
		return filepath.Join(w.root, "global-mcp.json"), nil
	}
	if rel == ".cursor/mcp.json" {
		return filepath.Join(w.root, ".cursor", "mcp.json"), nil
	}
	return rel, nil
}
