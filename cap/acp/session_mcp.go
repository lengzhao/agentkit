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

// SessionMCPServer describes one harness-provided MCP server for an ACP session.
// Additional transports (HTTP/SSE/ACP inline) may be added when virtual tools land.
type SessionMCPServer struct {
	Stdio *StdioMCPServer
}

// SessionMCPProvider supplies per-turn MCP servers for agent/acp-remote.
// Implementations should read session context from ctx (TurnEnvelope, workspace, etc.).
// This is separate from project mcp.json, which remote agents load on their own.
type SessionMCPProvider interface {
	SessionMCPServers(ctx context.Context) ([]SessionMCPServer, error)
}
