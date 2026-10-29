package sessionmcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/lengzhao/agentkit"
	capacp "github.com/lengzhao/agentkit/cap/acp"
	"github.com/lengzhao/agentkit/runtime/rctx"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const scopeHeader = "X-Agentkit-Mcp-Scope"

type scopeIdentity struct {
	Session   agentkit.SessionID
	Agent     agentkit.AgentID
	Workspace string
	Binding   string
}

func scopeKey(ctx context.Context) string {
	identity := scopeIdentity{Session: rctx.SessionIDFromContext(ctx), Agent: rctx.AgentIDFromContext(ctx), Binding: capacp.SessionMCPBindingFromContext(ctx)}
	// A bridge keeps its MCP configuration across turns, including workspace
	// changes. Its unique binding owns transport identity; workspace remains part
	// of the current turn context, not a reason to mint an unreachable token.
	if identity.Binding == "" {
		identity.Workspace = rctx.WorkspaceFromContext(ctx)
	}
	raw, _ := json.Marshal(identity)
	return string(raw)
}

type runtimeTurn struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
}
type runtimeScope struct {
	token   string
	runtime agentkit.ToolRuntime
	mcp     *server.MCPServer
	stream  *server.StreamableHTTPServer
	gate    chan struct{}
	mu      sync.Mutex
	active  *runtimeTurn
	catalog string
	closed  bool
}
type runtimeRequestKey struct{}
type runtimeRequest struct {
	scope *runtimeScope
	turn  *runtimeTurn
}

func (p *Provider) scope(ctx context.Context, create bool) (*runtimeScope, error) {
	if rctx.SessionIDFromContext(ctx) == "" {
		return nil, fmt.Errorf("runtime MCP requires session identity")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, fmt.Errorf("runtime MCP provider closed")
	}
	key := scopeKey(ctx)
	if s := p.scopes[key]; s != nil {
		return s, nil
	}
	if !create {
		return nil, fmt.Errorf("runtime MCP session has no active turn binding")
	}
	s := &runtimeScope{token: uuid.NewString(), runtime: p.runtime, gate: make(chan struct{}, 1)}
	s.mcp = server.NewMCPServer(p.serverName, "1.0.0", server.WithToolCapabilities(true))
	s.stream = server.NewStreamableHTTPServer(s.mcp)
	p.scopes[key] = s
	return s, nil
}

// BeginMCPTurn keeps transport identity stable while replacing execution state.
// Peers without a per-request turn ID are routed by session; a late request that
// arrives during the next turn cannot be distinguished from that turn's work.
func (p *Provider) BeginMCPTurn(ctx context.Context) (context.Context, func() error, error) {
	if p.runtime == nil {
		return ctx, func() error { return nil }, nil
	}
	s, err := p.scope(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	turnCtx, cancel := context.WithCancelCause(rctx.WithState(ctx, rctx.NewState()))
	t := &runtimeTurn{ctx: turnCtx, cancel: cancel}
	s.mu.Lock()
	if s.closed || s.active != nil {
		s.mu.Unlock()
		cancel(context.Canceled)
		return nil, nil, fmt.Errorf("runtime MCP session closed or already has an active turn")
	}
	s.active = t
	s.mu.Unlock()
	var once sync.Once
	var endErr error
	end := func() error {
		once.Do(func() {
			cause := context.Cause(turnCtx)
			if cause != nil {
				endErr = cause
			}
			cancel(context.Canceled)
			s.mu.Lock()
			if s.active == t {
				s.active = nil
			}
			s.mu.Unlock()
		})
		return endErr
	}
	if err = s.lock(turnCtx); err == nil {
		err = s.sync(turnCtx, t)
		s.unlock()
	}
	if err != nil {
		end()
		return nil, nil, err
	}
	return turnCtx, end, nil
}
func (s *runtimeScope) lock(ctx context.Context) error {
	select {
	case s.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *runtimeScope) unlock() { <-s.gate }

// sync runs with gate held. Only the matching live turn may publish a catalog.
func (s *runtimeScope) sync(ctx context.Context, t *runtimeTurn) error {
	specs, err := s.runtime.Visible(ctx)
	if err != nil {
		return fmt.Errorf("runtime MCP visible: %w", err)
	}
	entries := make([]server.ServerTool, 0, len(specs))
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if spec.Name == "" || seen[spec.Name] {
			return fmt.Errorf("runtime MCP invalid or duplicate tool name %q", spec.Name)
		}
		seen[spec.Name] = true
		schema := spec.InputSchema
		var raw []byte
		if len(schema.Raw) > 0 {
			raw, err = json.Marshal(schema.Raw)
		} else {
			raw, err = json.Marshal(schema)
		}
		if err != nil {
			return fmt.Errorf("runtime MCP schema %s: %w", spec.Name, err)
		}
		entries = append(entries, server.ServerTool{Tool: mcp.NewToolWithRawSchema(spec.Name, spec.Description, raw), Handler: s.handler(spec.Name)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Tool.Name < entries[j].Tool.Name })
	tools := make([]mcp.Tool, 0, len(entries))
	for _, entry := range entries {
		tools = append(tools, entry.Tool)
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != t || t.ctx.Err() != nil {
		return fmt.Errorf("runtime MCP turn ended")
	}
	if string(raw) != s.catalog {
		s.mcp.SetTools(entries...)
		s.catalog = string(raw)
	}
	return nil
}
func (s *runtimeScope) handler(name string) server.ToolHandlerFunc {
	return func(requestCtx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		binding, _ := requestCtx.Value(runtimeRequestKey{}).(runtimeRequest)
		if binding.scope != s || binding.turn == nil {
			return mcp.NewToolResultError("runtime MCP has no active turn"), nil
		}
		t := binding.turn
		ctx, cancel := context.WithCancel(t.ctx)
		defer cancel()
		stop := context.AfterFunc(requestCtx, cancel)
		defer stop()
		if err := s.lock(ctx); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		defer s.unlock()
		s.mu.Lock()
		active := s.active == t && !s.closed
		s.mu.Unlock()
		if !active || ctx.Err() != nil {
			return mcp.NewToolResultError("runtime MCP turn ended"), nil
		}
		raw, err := json.Marshal(req.Params.Arguments)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		call := agentkit.ToolCall{ID: agentkit.ToolCallID(uuid.NewString()), Name: name, Input: raw}
		result, err := s.runtime.Execute(ctx, call)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if agentkit.IsTurnAbort(err) {
			t.cancel(err)
		}
		if err != nil {
			text := err.Error()
			if result.Content != "" {
				text = result.Content + "\n" + text
			}
			return mcp.NewToolResultError(text), nil
		}
		if err = s.sync(ctx, t); err != nil {
			return mcp.NewToolResultError(result.Content + "\n" + err.Error()), nil
		}
		switch result.Audit["decision"] {
		case "deny", "timeout", "error":
			return mcp.NewToolResultError(result.Content), nil
		}
		return mcp.NewToolResultText(result.Content), nil
	}
}
func (p *Provider) runtimeServers(ctx context.Context) ([]capacp.SessionMCPServer, error) {
	s, err := p.scope(ctx, true)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	url := p.baseURL + "/mcp"
	p.mu.Unlock()
	headers := make(map[string]string, len(p.staticHeaders)+2)
	for k, v := range p.staticHeaders {
		headers[http.CanonicalHeaderKey(k)] = v
	}
	headers[scopeHeader] = s.token
	if p.apiToken != "" {
		headers["Authorization"] = "Bearer " + p.apiToken
	}
	return []capacp.SessionMCPServer{{HTTP: &capacp.HTTPMCPServer{Name: p.serverName, URL: url, Type: defaultTransportType, Headers: headers}}}, nil
}
func (p *Provider) serveRuntime(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get(scopeHeader)
	p.mu.Lock()
	var scope *runtimeScope
	for _, s := range p.scopes {
		if s.token == token {
			scope = s
			break
		}
	}
	p.mu.Unlock()
	if scope == nil {
		http.Error(w, "unknown runtime MCP scope", http.StatusUnauthorized)
		return
	}
	scope.mu.Lock()
	t := scope.active
	closed := scope.closed
	scope.mu.Unlock()
	if closed {
		http.Error(w, "runtime MCP scope closed", http.StatusGone)
		return
	}
	ctx := context.WithValue(r.Context(), runtimeRequestKey{}, runtimeRequest{scope, t})
	scope.stream.ServeHTTP(w, r.WithContext(ctx))
}
func (p *Provider) refreshRuntime(ctx context.Context) error {
	s, err := p.scope(ctx, false)
	if err != nil {
		return err
	}
	s.mu.Lock()
	t := s.active
	s.mu.Unlock()
	if t == nil {
		return fmt.Errorf("runtime MCP has no active turn")
	}
	callCtx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	if err = s.lock(callCtx); err != nil {
		return err
	}
	defer s.unlock()
	return s.sync(callCtx, t)
}
func (p *Provider) ReleaseMCPSession(ctx context.Context) {
	if p.runtime == nil {
		return
	}
	p.mu.Lock()
	key := scopeKey(ctx)
	s := p.scopes[key]
	delete(p.scopes, key)
	p.mu.Unlock()
	if s != nil {
		s.close()
	}
}
func (s *runtimeScope) close() {
	s.mu.Lock()
	s.closed = true
	if s.active != nil {
		s.active.cancel(errors.New("runtime MCP session closed"))
		s.active = nil
	}
	s.mu.Unlock()
	_ = s.stream.Shutdown(context.Background())
}
