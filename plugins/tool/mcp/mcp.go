package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/credentials"
	"github.com/lengzhao/agentkit/cap/workspace"
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
	defs, err := p.listToolDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]agentkit.Tool, 0, len(defs))
	for _, def := range defs {
		out = append(out, &mcpTool{def: def, provider: p})
	}
	return out, nil
}

func (p *mcpProvider) listToolDefinitions(ctx context.Context) ([]toolDefinition, error) {
	servers, err := p.loadServers(ctx)
	if err != nil {
		return nil, err
	}
	var out []toolDefinition
	for _, server := range servers {
		tools, err := p.pool.tools(ctx, server, p.credentials)
		if err != nil {
			slogWarnMCPServer(server.Name, err)
			continue
		}
		out = append(out, tools...)
	}
	return out, nil
}

func (p *mcpProvider) callTool(ctx context.Context, exposedName string, input json.RawMessage) (mcpCallOutcome, error) {
	exposedName = strings.TrimSpace(exposedName)
	if exposedName == "" {
		return mcpCallOutcome{}, fmt.Errorf("mcp tool name is required")
	}
	servers, err := p.loadServers(ctx)
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
