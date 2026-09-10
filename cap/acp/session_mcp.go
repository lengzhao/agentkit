package acp

import "context"

// StdioMCPServer is stdio transport configuration for one MCP server passed to
// an external ACP agent at session/new or session/resume.
type StdioMCPServer struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

// HTTPMCPServer is HTTP transport configuration (requires agent mcp_capabilities.http).
type HTTPMCPServer struct {
	Name    string
	URL     string
	Type    string
	Headers map[string]string
}

// SSEMCPServer is SSE transport configuration (requires agent mcp_capabilities.sse).
type SSEMCPServer struct {
	Name    string
	URL     string
	Type    string
	Headers map[string]string
}

// SessionMCPServer describes one harness-provided MCP server for an ACP session.
// Set exactly one of Stdio, HTTP, or SSE. ACP inline transport may be added later.
type SessionMCPServer struct {
	Stdio *StdioMCPServer
	HTTP  *HTTPMCPServer
	SSE   *SSEMCPServer
}

// SessionMCPProvider supplies per-turn MCP servers for agent/acp-remote.
// Implementations should read session context from ctx (TurnEnvelope, workspace, etc.).
// This is separate from project mcp.json, which remote agents load on their own.
type SessionMCPProvider interface {
	SessionMCPServers(ctx context.Context) ([]SessionMCPServer, error)
}
