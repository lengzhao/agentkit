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

// SessionMCPTurnProvider optionally binds an MCP connection to a host turn.
// Begin must run on every turn, including when the remote ACP session is reused.
// The returned context is used for session setup and prompting. End is idempotent,
// cancels tool work, and returns any turn-fatal tool error.
type SessionMCPTurnProvider interface {
	BeginMCPTurn(context.Context) (context.Context, func() error, error)
}

// SessionMCPReleaser releases connection-scoped resources when an ACP bridge is
// retired. The context carries the same session/agent/workspace identity as Begin.
type SessionMCPReleaser interface {
	ReleaseMCPSession(context.Context)
}

// sessionMCPBindingKey separates connection ownership from conversation identity.
type sessionMCPBindingKey struct{}

// WithSessionMCPBinding identifies one ACP bridge lifetime. A replacement bridge
// must use a new ID so delayed cleanup cannot revoke its successor's resources.
func WithSessionMCPBinding(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionMCPBindingKey{}, id)
}
func SessionMCPBindingFromContext(ctx context.Context) string {
	id, _ := ctx.Value(sessionMCPBindingKey{}).(string)
	return id
}
