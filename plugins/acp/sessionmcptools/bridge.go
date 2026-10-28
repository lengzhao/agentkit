package sessionmcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/rctx"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// toolBridge mirrors a ToolProvider into an MCP server registry.
type toolBridge struct {
	provider agentkit.ToolProvider
	mcp      *server.MCPServer
	mu       sync.Mutex
	tools    map[string]agentkit.Tool
}

func newToolBridge(provider agentkit.ToolProvider) *toolBridge {
	return &toolBridge{provider: provider, tools: make(map[string]agentkit.Tool)}
}

func (b *toolBridge) bind(mcp *server.MCPServer) { b.mcp = mcp }

// sync mirrors the provider's current tools, adding new ones and deleting
// vanished ones, so a provider that reloads at runtime stays in step.
func (b *toolBridge) sync(ctx context.Context) error {
	if b.mcp == nil {
		return fmt.Errorf("mcp server not bound")
	}
	tools, err := b.provider.ListTools(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]agentkit.Tool, len(tools))
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		name := strings.TrimSpace(tool.Name())
		if name == "" {
			continue
		}
		next[name] = tool
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for name := range b.tools {
		if next[name] == nil {
			b.mcp.DeleteTools(name)
		}
	}
	for name, tool := range next {
		b.mcp.AddTool(mcplib.NewToolWithRawSchema(name, tool.Description(), rawInputSchema(tool)), b.handler(name))
	}
	b.tools = next
	return nil
}

func (b *toolBridge) handler(name string) func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		tool, err := b.toolForCall(name)
		if err != nil {
			return mcplib.NewToolResultError(err.Error()), nil
		}
		raw, err := json.Marshal(req.Params.Arguments)
		if err != nil {
			return mcplib.NewToolResultError(err.Error()), nil
		}
		out, err := tool.Call(ctx, raw)
		if err != nil {
			return mcplib.NewToolResultError(err.Error()), nil
		}
		return mcplib.NewToolResultText(out), nil
	}
}

func (b *toolBridge) toolForCall(name string) (agentkit.Tool, error) {
	b.mu.Lock()
	tool := b.tools[name]
	b.mu.Unlock()
	if tool == nil {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	return tool, nil
}

func rawInputSchema(tool agentkit.Tool) json.RawMessage {
	schema := tool.InputSchema()
	if len(schema.Raw) > 0 {
		if raw, err := json.Marshal(schema.Raw); err == nil {
			return raw
		}
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return raw
}

// parseBinds validates and normalizes header binds.
func parseBinds(raw map[string]HeaderBind) ([]HeaderBind, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[string]string, len(raw))
	out := make([]HeaderBind, 0, len(raw))
	for key, entry := range raw {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		from := strings.TrimSpace(entry.From)
		if from == "" {
			return nil, fmt.Errorf("bind %q: from is required", key)
		}
		if !strings.HasPrefix(from, "ctx:") {
			return nil, fmt.Errorf("bind %q: from %q must use ctx: prefix", key, from)
		}
		in := strings.ToLower(strings.TrimSpace(entry.In))
		if in == "" {
			in = "header"
		}
		if in != "header" {
			return nil, fmt.Errorf("bind %q: unsupported in %q (only header is supported)", key, entry.In)
		}
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = key
		}
		canon := http.CanonicalHeaderKey(name)
		if prior, ok := seen[strings.ToLower(canon)]; ok {
			return nil, fmt.Errorf("bind %q conflicts with bind %q on header %q", key, prior, canon)
		}
		seen[strings.ToLower(canon)] = key
		out = append(out, HeaderBind{From: from, In: in, Name: name})
	}
	return out, nil
}

func headerName(bind HeaderBind) string {
	if name := strings.TrimSpace(bind.Name); name != "" {
		return http.CanonicalHeaderKey(name)
	}
	return http.CanonicalHeaderKey(bind.From)
}

func bearerAuthorized(r *http.Request, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")) == token
}

// contextFromInboundHeaders rebuilds the turn envelope from request headers, so
// a tool invoked over MCP sees the same session/user/workspace it would
// in-process. ctx:route carries the full RouteRef as JSON: without it a tool
// that stores a destination (tool/schedule) would persist a route with no
// target and could never deliver.
func contextFromInboundHeaders(r *http.Request, binds []HeaderBind, ws workspace.Service) context.Context {
	ctx := r.Context()
	env := rctx.EnvelopeFromContext(ctx)
	for _, bind := range binds {
		val := strings.TrimSpace(r.Header.Get(headerName(bind)))
		if val == "" {
			continue
		}
		key := strings.TrimPrefix(strings.TrimSpace(bind.From), "ctx:")
		switch {
		case key == "session_id":
			env.Conversation = val
		case key == "user_id":
			env.Actor.UserID = val
		case key == "agent_id":
			env.AgentID = agentkit.AgentID(val)
		case key == "platform_id":
			env.Route.Platform = val
		case key == "tenant":
			env.Workspace = val
		case key == "route":
			var route agentkit.RouteRef
			if err := json.Unmarshal([]byte(val), &route); err == nil {
				env.Route = route
			} else {
				env.Route.Platform = val
			}
		case strings.HasPrefix(key, "metadata."):
			mdKey := strings.TrimPrefix(key, "metadata.")
			if mdKey != "" {
				env = rctx.MergeEnvelopeMetadata(env, map[string]any{mdKey: val})
			}
		default:
			continue
		}
	}
	ctx = rctx.ApplyEnvelopeToContext(ctx, env)
	if ws != nil {
		ctx = rctx.WithWorkspaceService(ctx, ws)
	}
	return ctx
}
