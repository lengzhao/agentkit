// Package sessionmcptools serves any agentkit.ToolProvider to an external ACP
// agent as a loopback streamable-http MCP server.
//
// agent/acp-remote's sessionMcp slot takes a capacp.SessionMCPProvider, but the
// tools a host wants to expose usually already exist as a ToolProvider
// (tool/openapi, tool/mcp, a static tool list, ...). This plugin is the general
// bridge between the two, so a host does not need one bespoke MCP server per
// tool source.
package sessionmcptools

import (
	"context"
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
	"github.com/lengzhao/pluginkit"
	"github.com/mark3labs/mcp-go/server"
)

const (
	defaultServerName    = "harness-tools"
	defaultTransportType = "streamable-http"
)

func init() {
	pluginkit.Register("acp/session-mcp-tools", New)
}

// Config configures the loopback MCP endpoint.
type Config struct {
	// ListenAddr defaults to 127.0.0.1:0 (ephemeral port).
	ListenAddr string `json:"listenAddr,omitempty"`
	// APIToken enables a loopback Bearer check. Empty disables it (local only).
	APIToken string `json:"apiToken,omitempty"`
	// ServerName is the MCP server name announced to the agent.
	ServerName string `json:"serverName,omitempty"`
	// TransportType defaults to streamable-http.
	TransportType string `json:"transportType,omitempty"`
	// Headers are static extra headers for the ACP session/new spec.
	Headers map[string]string `json:"headers,omitempty"`
	// Bind maps request headers to turn-context fields, so a tool call that
	// arrives over MCP sees the same envelope (session, user, workspace) it
	// would see in-process.
	Bind map[string]HeaderBind `json:"bind,omitempty"`
}

// HeaderBind mirrors the header-bind shape hosts already use: From is a ctx:
// source, In is the transport slot, Name the wire name.
type HeaderBind struct {
	From string `json:"from"`
	In   string `json:"in"`
	Name string `json:"name"`
}

// Deps wires the tool source and the workspace used to resolve paths.
type Deps struct {
	Provider  agentkit.ToolProvider `json:"provider"`
	Workspace workspace.Service     `json:"workspace,omitempty"`
}

// Provider implements capacp.SessionMCPProvider over the supplied ToolProvider.
type Provider struct {
	apiToken      string
	serverName    string
	transportType string
	staticHeaders map[string]string
	binds         []HeaderBind
	bridge        *toolBridge
	workspace     workspace.Service

	mu       sync.Mutex
	baseURL  string
	listener net.Listener
	server   *http.Server
}

// New registers acp/session-mcp-tools: streamable-http MCP over any ToolProvider.
func New(cfg Config, deps Deps) (capacp.SessionMCPProvider, error) {
	if deps.Provider == nil {
		return nil, fmt.Errorf("acp/session-mcp-tools requires a provider dependency")
	}
	binds, err := parseBinds(cfg.Bind)
	if err != nil {
		return nil, err
	}
	serverName := strings.TrimSpace(cfg.ServerName)
	if serverName == "" {
		serverName = defaultServerName
	}
	transport := strings.TrimSpace(cfg.TransportType)
	if transport == "" {
		transport = defaultTransportType
	}
	p := &Provider{
		apiToken:      strings.TrimSpace(cfg.APIToken),
		serverName:    serverName,
		transportType: transport,
		staticHeaders: cfg.Headers,
		binds:         binds,
		bridge:        newToolBridge(deps.Provider),
		workspace:     deps.Workspace,
	}
	if err := p.startHTTP(strings.TrimSpace(cfg.ListenAddr)); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Provider) startHTTP(listenAddr string) error {
	if listenAddr == "" {
		listenAddr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("acp/session-mcp-tools listen %s: %w", listenAddr, err)
	}
	mcp := server.NewMCPServer(p.serverName, "1.0.0", server.WithToolCapabilities(true))
	p.bridge.bind(mcp)
	if err := p.bridge.sync(context.Background()); err != nil {
		_ = ln.Close()
		return fmt.Errorf("acp/session-mcp-tools tools: %w", err)
	}
	stream := server.NewStreamableHTTPServer(mcp)
	mux := http.NewServeMux()
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !bearerAuthorized(r, p.apiToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
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
	p.listener = ln
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
	return p.bridge.sync(ctx)
}

// SessionMCPServers implements capacp.SessionMCPProvider.
func (p *Provider) SessionMCPServers(ctx context.Context) ([]capacp.SessionMCPServer, error) {
	headers := make(map[string]string, len(p.staticHeaders)+1)
	for k, v := range p.staticHeaders {
		headers[k] = v
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
			Type:    p.transportType,
			Headers: headers,
		},
	}}, nil
}

// Close stops the loopback server.
func (p *Provider) Close() error {
	p.mu.Lock()
	srv := p.server
	p.mu.Unlock()
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
