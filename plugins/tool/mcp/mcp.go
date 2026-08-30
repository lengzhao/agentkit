package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/plugins/configfile"
)

const defaultGlobalMCPFile = "global:mcp.json"

var defaultMCPFiles = []string{".cursor/mcp.json", defaultGlobalMCPFile}

type MCPConfig struct {
	// Files are MCP config paths in precedence order; first file wins for duplicate server names.
	Files []string `json:"files,omitempty"`
}

type MCPDeps struct {
	Workspace   workspace.Service `json:"workspace"`
	Credentials credentials.Store `json:"credentials,omitempty"`
}

type mcpProvider struct {
	files       []string
	workspace   workspace.Service
	credentials credentials.Store
	pool        *clientPool

	mu      sync.RWMutex
	loaded  bool
	servers []serverConfig
	defs    []toolDefinition
}

type mcpTool struct {
	def      toolDefinition
	provider *mcpProvider
}

// NewMCP registers tool/mcp: Load mcpServers JSON and expose MCP tools as dynamic model-visible tools.
//
// Best practices:
//   - Put project servers in .cursor/mcp.json; use global:mcp.json for cross-project defaults.
//   - Mount via tools/runtime deps.dynamicTools, not deps.tools, because definitions are discovered at runtime.
//   - Server configs and their tool lists are loaded once and cached; run the "mcp" command to reload mcp.json and rediscover tools after editing it or restarting a server.
func NewMCP(cfg MCPConfig, deps MCPDeps) (agentkit.ToolProvider, error) {
	if deps.Workspace == nil {
		return nil, fmt.Errorf("tool/mcp requires workspace")
	}
	files := cfg.Files
	if len(files) == 0 {
		files = defaultMCPFiles
	}
	return &mcpProvider{
		files:       files,
		workspace:   deps.Workspace,
		credentials: deps.Credentials,
		pool:        newClientPool(),
	}, nil
}

func (p *mcpProvider) ListTools(ctx context.Context) ([]agentkit.Tool, error) {
	_, defs, err := p.cached(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]agentkit.Tool, 0, len(defs))
	for _, def := range defs {
		out = append(out, &mcpTool{def: def, provider: p})
	}
	return out, nil
}

// cached returns the last loaded server configs and tool definitions,
// loading them once on first use. They are not re-read from disk, nor
// re-discovered over the wire from each MCP server, again until reload runs
// (via the "mcp" command), so repeated ListTools calls don't pay for a
// config read plus a live ListTools RPC per server on every turn.
func (p *mcpProvider) cached(ctx context.Context) ([]serverConfig, []toolDefinition, error) {
	p.mu.RLock()
	loaded := p.loaded
	servers, defs := p.servers, p.defs
	p.mu.RUnlock()
	if loaded {
		return servers, defs, nil
	}
	return p.reload(ctx)
}

// reload re-reads every mcp.json file from disk and re-queries each
// configured server's tool list, then replaces the cache. It backs the
// "mcp -u" command.
func (p *mcpProvider) reload(ctx context.Context) ([]serverConfig, []toolDefinition, error) {
	servers, err := p.loadServers(ctx)
	if err != nil {
		return nil, nil, err
	}
	defs, err := p.discoverTools(ctx, servers)
	if err != nil {
		return nil, nil, err
	}
	p.mu.Lock()
	p.servers = servers
	p.defs = defs
	p.loaded = true
	p.mu.Unlock()
	return servers, defs, nil
}

func (p *mcpProvider) discoverTools(ctx context.Context, servers []serverConfig) ([]toolDefinition, error) {
	var defs []toolDefinition
	for _, server := range servers {
		tools, err := p.pool.tools(ctx, server, p.credentials)
		if err != nil {
			slogWarnMCPServer(server.Name, err)
			continue
		}
		defs = append(defs, tools...)
	}
	return defs, nil
}

func (p *mcpProvider) writeTarget(ctx context.Context) (string, error) {
	rel, err := configfile.WriteTarget(p.files)
	if err != nil {
		return "", err
	}
	return p.workspace.Resolve(ctx, rel)
}

func (p *mcpProvider) addServer(ctx context.Context, name string, raw []byte) (string, error) {
	cfg, err := parseServerJSON(name, "command:add", raw)
	if err != nil {
		return "", err
	}
	tools, err := p.pool.tools(ctx, cfg, p.credentials)
	if err != nil {
		return "", fmt.Errorf("mcp server %q probe failed: %w", name, err)
	}

	target, err := p.writeTarget(ctx)
	if err != nil {
		return "", err
	}
	prev, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", target, err)
	}
	var prevBytes []byte
	if err == nil {
		prevBytes = append([]byte(nil), prev...)
	}
	merged, err := upsertMCPJSON(prevBytes, name, raw)
	if err != nil {
		return "", err
	}
	if err := configfile.WriteAtomic(target, merged, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", target, err)
	}
	if _, defs, err := p.reload(ctx); err != nil {
		_ = configfile.Restore(target, prevBytes, 0o644)
		_, _, _ = p.reload(ctx)
		return "", err
	} else if !serverToolsPresent(defs, name, len(tools)) {
		_ = configfile.Restore(target, prevBytes, 0o644)
		_, _, _ = p.reload(ctx)
		return "", fmt.Errorf("mcp server %q failed validation after reload", name)
	}
	return fmt.Sprintf("mcp: wrote %s to %s (%d tools), verified", name, target, len(tools)), nil
}

func serverToolsPresent(defs []toolDefinition, server string, want int) bool {
	count := 0
	for _, def := range defs {
		if def.Server == server {
			count++
		}
	}
	return count == want
}

func (p *mcpProvider) statusWithHelp(ctx context.Context) (string, error) {
	_, defs, err := p.cached(ctx)
	if err != nil {
		return "", err
	}
	return formatMCPStatus(defs) + "\n\n" + mcpHelp(), nil
}

func formatMCPStatus(defs []toolDefinition) string {
	if len(defs) == 0 {
		return "mcp: 0 tools loaded"
	}
	counts := make(map[string]int)
	var order []string
	for _, def := range defs {
		if _, ok := counts[def.Server]; !ok {
			order = append(order, def.Server)
		}
		counts[def.Server]++
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, fmt.Sprintf("%s (%d tools)", name, counts[name]))
	}
	return fmt.Sprintf("mcp: %d tool(s) from %d server(s): %s", len(defs), len(order), strings.Join(parts, ", "))
}

func mcpHelp() string {
	return `Usage:
  /mcp                         show status and help
  /mcp add <name> <json>       write server to mcp.json, probe, reload, and verify
  /mcp -u                      reload mcp.json and rediscover tools

JSON format matches one mcpServers entry, e.g.:
  {"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","/path"]}
  {"url":"http://127.0.0.1:8080/mcp","type":"sse"}
  {"url":"http://127.0.0.1:8080/mcp","bind":{"X-User-Id":{"from":"ctx:user_id","in":"header"}}}

Notes:
  add writes to the local mcp.json file (config.files local: entry)
  See docs/guides/tools.zh.md for full mcp.json format`
}

func (p *mcpProvider) callTool(ctx context.Context, exposedName string, input json.RawMessage) (mcpCallOutcome, error) {
	exposedName = strings.TrimSpace(exposedName)
	if exposedName == "" {
		return mcpCallOutcome{}, fmt.Errorf("mcp tool name is required")
	}
	servers, _, err := p.cached(ctx)
	if err != nil {
		return mcpCallOutcome{}, err
	}
	for _, server := range servers {
		prefix := server.toolPrefix()
		if !strings.HasPrefix(exposedName, prefix) {
			continue
		}
		original := strings.TrimPrefix(exposedName, prefix)
		if original == "" {
			continue
		}
		if !server.allowsTool(original) {
			continue
		}
		return p.pool.call(ctx, server, original, input, p.credentials)
	}
	return mcpCallOutcome{}, fmt.Errorf("unknown mcp tool %q", exposedName)
}

func (p *mcpProvider) loadServers(ctx context.Context) ([]serverConfig, error) {
	seen := make(map[string]struct{})
	var out []serverConfig
	for _, rel := range p.files {
		path, err := p.workspace.Resolve(ctx, rel)
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			slogWarnMCPFile(path, err)
			continue
		}
		servers, err := parseConfigFile(path, raw)
		if err != nil {
			slogWarnMCPFile(path, err)
			continue
		}
		for _, server := range servers {
			if _, ok := seen[server.Name]; ok {
				continue
			}
			seen[server.Name] = struct{}{}
			out = append(out, server)
		}
	}
	return out, nil
}

// Commands contributes the "mcp" slash command that forces a reload.
func (p *mcpProvider) Commands() []agentkit.Command {
	return []agentkit.Command{&mcpSyncCommand{provider: p}}
}

type mcpSyncCommand struct {
	provider *mcpProvider
}

func (c *mcpSyncCommand) Name() string  { return "mcp" }
func (c *mcpSyncCommand) Alias() string { return "" }
func (c *mcpSyncCommand) Description() string {
	return "Show MCP tools, add a server from JSON, or reload mcp.json with -u"
}

func (c *mcpSyncCommand) CommandExec(ctx context.Context, args string) (string, error) {
	update, rest := peelUpdateFlag(strings.Fields(strings.TrimSpace(args)))
	switch {
	case update:
		_, defs, err := c.provider.reload(ctx)
		if err != nil {
			return "", err
		}
		return summarizeMCPTools(defs), nil
	case len(rest) >= 1 && rest[0] == "add":
		if len(rest) < 3 {
			return "", fmt.Errorf("usage: /mcp add <name> <json>")
		}
		name := strings.TrimSpace(rest[1])
		if name == "" {
			return "", fmt.Errorf("server name is required")
		}
		return c.provider.addServer(ctx, name, []byte(strings.Join(rest[2:], " ")))
	case len(rest) == 0:
		return c.provider.statusWithHelp(ctx)
	default:
		return "", fmt.Errorf("usage: /mcp | /mcp add <name> <json> | /mcp -u")
	}
}

func peelUpdateFlag(args []string) (update bool, rest []string) {
	for _, arg := range args {
		switch arg {
		case "-u", "--update":
			update = true
		default:
			rest = append(rest, arg)
		}
	}
	return update, rest
}

func summarizeMCPTools(defs []toolDefinition) string {
	if len(defs) == 0 {
		return "mcp: reloaded, 0 tools discovered"
	}
	counts := make(map[string]int)
	var order []string
	for _, def := range defs {
		if _, ok := counts[def.Server]; !ok {
			order = append(order, def.Server)
		}
		counts[def.Server]++
	}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, fmt.Sprintf("%s (%d tools)", name, counts[name]))
	}
	return fmt.Sprintf("mcp: reloaded %d tool(s) from %d server(s): %s", len(defs), len(order), strings.Join(parts, ", "))
}

var _ agentkit.CommandProvider = (*mcpProvider)(nil)

func (t *mcpTool) Name() string { return t.def.ExposedName }

func (t *mcpTool) Description() string {
	if t.def.Description != "" {
		return t.def.Description
	}
	return "MCP tool " + t.def.OriginalName + " from server " + t.def.Server
}

func (t *mcpTool) InputSchema() agentkit.JSONSchema {
	if len(t.def.InputSchema) == 0 {
		return agentkit.JSONSchema{Type: "object"}
	}
	var raw map[string]any
	if err := json.Unmarshal(t.def.InputSchema, &raw); err != nil {
		return agentkit.JSONSchema{Type: "object"}
	}
	return agentkit.JSONSchema{Raw: raw}
}

func (t *mcpTool) Call(ctx context.Context, input json.RawMessage) (string, error) {
	result, err := t.provider.callTool(ctx, t.def.ExposedName, input)
	if err != nil {
		return err.Error(), nil
	}
	return result.Content, nil
}
