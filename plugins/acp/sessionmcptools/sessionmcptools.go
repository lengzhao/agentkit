// Package sessionmcptools exposes a ToolProvider or ToolRuntime to an external
// ACP agent as a loopback streamable-http MCP server. Runtime mode preserves
// turn state and the policy/approval pipeline through cap/acp's turn lifecycle.
package sessionmcptools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lengzhao/agentkit"
	capacp "github.com/lengzhao/agentkit/cap/acp"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/bind"
	"github.com/lengzhao/pluginkit"
	"github.com/mark3labs/mcp-go/server"
)

const (
	executionCwdHeader   = "X-Agentkit-Execution-Cwd"
	defaultServerName    = "harness-tools"
	defaultTransportType = "streamable-http"
)

func init() {
	pluginkit.Register("acp/session-mcp-tools", New)
}

// Config configures the loopback MCP endpoint.
type Config struct {
	// APIToken enables a loopback Bearer check. Empty disables it (local only).
	APIToken string `json:"apiToken,omitempty"`
	// ServerName is the MCP server name announced to the agent.
	ServerName string `json:"serverName,omitempty"`
	// Headers are static extra headers for the ACP session/new spec.
	Headers map[string]string `json:"headers,omitempty"`
	// Bind maps turn-context fields to session headers and restores them on
	// inbound tool calls, preserving the session, user, workspace and route.
	Bind map[string]HeaderBind `json:"bind,omitempty"`
}

// HeaderBind mirrors the header-bind shape hosts already use: From is a ctx:
// source, In is the transport slot, Name the wire name.
type HeaderBind struct {
	From string `json:"from"`
	In   string `json:"in"`
	Name string `json:"name"`
}

// Deps selects exactly one tool source and the workspace used to resolve paths.
type Deps struct {
	// Provider is the legacy direct-tool backend; mutually exclusive with Runtime.
	Provider agentkit.ToolProvider `json:"provider,omitempty"`
	// Runtime preserves visibility, policy, approval, and execution semantics.
	Runtime   agentkit.ToolRuntime `json:"runtime,omitempty"`
	Workspace workspace.Service    `json:"workspace,omitempty"`
}

// Provider implements SessionMCPProvider and the optional turn lifecycle.
type Provider struct {
	apiToken      string
	serverName    string
	staticHeaders map[string]string
	binds         []HeaderBind
	bridge        *toolBridge
	workspace     workspace.Service
	runtime       agentkit.ToolRuntime
	scopes        map[string]*runtimeScope
	closed        bool

	mu      sync.Mutex
	baseURL string
	server  *http.Server
}

// New registers acp/session-mcp-tools: streamable-http MCP over a tool provider or runtime.
func New(cfg Config, deps Deps) (capacp.SessionMCPProvider, error) {
	if (deps.Provider == nil) == (deps.Runtime == nil) {
		return nil, fmt.Errorf("acp/session-mcp-tools requires exactly one provider or runtime dependency")
	}
	binds, err := parseBinds(cfg.Bind)
	if err != nil {
		return nil, err
	}
	serverName := strings.TrimSpace(cfg.ServerName)
	if serverName == "" {
		serverName = defaultServerName
	}
	p := &Provider{
		apiToken:      strings.TrimSpace(cfg.APIToken),
		serverName:    serverName,
		staticHeaders: cfg.Headers,
		binds:         binds,
		bridge:        newToolBridge(deps.Provider),
		workspace:     deps.Workspace,
		runtime:       deps.Runtime,
		scopes:        make(map[string]*runtimeScope),
	}
	if err := p.startHTTP(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Provider) startHTTP() error {
	const listenAddr = "127.0.0.1:0"
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("acp/session-mcp-tools listen %s: %w", listenAddr, err)
	}
	var stream *server.StreamableHTTPServer
	if p.runtime == nil {
		mcp := server.NewMCPServer(p.serverName, "1.0.0", server.WithToolCapabilities(true), server.WithToolFilter(p.bridge.describeForRequest))
		p.bridge.bind(mcp)
		if err := p.bridge.sync(context.Background()); err != nil {
			_ = ln.Close()
			return fmt.Errorf("acp/session-mcp-tools tools: %w", err)
		}
		stream = server.NewStreamableHTTPServer(mcp)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !bearerAuthorized(r, p.apiToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if p.runtime != nil {
			p.serveRuntime(w, r)
			return
		}
		ctx := contextFromInboundHeaders(r, p.binds, p.workspace)
		// The tool registry is synced once at startup and on explicit Refresh.
		// Syncing on every request would call AddTool/DeleteTools against a
		// registry the MCP server is concurrently serving.
		stream.ServeHTTP(w, r.WithContext(ctx))
	}))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	p.mu.Lock()
	p.baseURL = "http://" + ln.Addr().String()
	p.server = srv
	p.mu.Unlock()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("acp/session-mcp-tools: serve failed", "err", err)
		}
	}()
	return nil
}

// Refresh re-reads the provider's tools into the MCP registry. Call it after a
// runtime change (an OpenAPI reload, an MCP client discovery); it is safe to
// call concurrently with in-flight calls.
func (p *Provider) Refresh(ctx context.Context) error {
	if p.runtime != nil {
		return p.refreshRuntime(ctx)
	}
	return p.bridge.sync(ctx)
}

// SessionMCPServers implements capacp.SessionMCPProvider.
func (p *Provider) SessionMCPServers(ctx context.Context) ([]capacp.SessionMCPServer, error) {
	if p.runtime != nil {
		return p.runtimeServers(ctx)
	}
	headers := make(map[string]string, len(p.staticHeaders)+2)
	for k, v := range p.staticHeaders {
		headers[http.CanonicalHeaderKey(k)] = v
	}
	for _, entry := range p.binds {
		value, err := bind.Default.ResolveCtxValue(ctx, entry.From)
		if err != nil {
			return nil, fmt.Errorf("session mcp header %s: %w", entry.Name, err)
		}
		// A missing context value must not reuse a static header from another scope.
		delete(headers, headerName(entry))
		if value != "" {
			headers[headerName(entry)] = value
		}
	}
	// Always replace the reserved snapshot header, including when unbound.
	delete(headers, executionCwdHeader)
	if cwd := workspace.WorkingDirectory(ctx); cwd != "" {
		headers[executionCwdHeader] = base64.StdEncoding.EncodeToString([]byte(cwd))
	}
	if p.apiToken != "" {
		headers[http.CanonicalHeaderKey("Authorization")] = "Bearer " + p.apiToken
	}
	p.mu.Lock()
	url := strings.TrimRight(p.baseURL, "/") + "/mcp"
	p.mu.Unlock()
	if url == "/mcp" {
		return nil, fmt.Errorf("acp/session-mcp-tools: server not started")
	}
	return []capacp.SessionMCPServer{{
		HTTP: &capacp.HTTPMCPServer{
			Name:    p.serverName,
			URL:     url,
			Type:    defaultTransportType,
			Headers: headers,
		},
	}}, nil
}

// Close stops the loopback server.
func (p *Provider) Close() error {
	p.mu.Lock()
	srv := p.server
	p.closed = true
	scopes := p.scopes
	p.scopes = make(map[string]*runtimeScope)
	p.mu.Unlock()
	for _, scope := range scopes {
		scope.close()
	}
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
