package sessionmcptools

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	capacp "github.com/lengzhao/agentkit/cap/acp"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

type stubTool struct{ name string }

func (s stubTool) Name() string                                          { return s.name }
func (s stubTool) Description() string                                   { return "stub " + s.name }
func (s stubTool) InputSchema() agentkit.JSONSchema                      { return agentkit.JSONSchema{Type: "object"} }
func (s stubTool) Call(context.Context, json.RawMessage) (string, error) { return "ok", nil }

func stubProvider(tools ...agentkit.Tool) agentkit.ToolProvider {
	return &stubProviderImpl{tools: tools}
}

type stubProviderImpl struct{ tools []agentkit.Tool }

func (p *stubProviderImpl) ListTools(context.Context) ([]agentkit.Tool, error) {
	return append([]agentkit.Tool(nil), p.tools...), nil
}

func TestNewRequiresProvider(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{}, Deps{}); err == nil {
		t.Fatal("expected an error without a provider")
	}
}

func TestNewServesLoopbackMCP(t *testing.T) {
	t.Parallel()

	p, err := New(Config{ServerName: "sched"}, Deps{Provider: stubProvider(stubTool{"now"})})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProvider(t, p)

	servers, err := p.SessionMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].HTTP == nil {
		t.Fatalf("servers = %+v", servers)
	}
	spec := servers[0].HTTP
	if spec.Name != "sched" {
		t.Fatalf("name = %q", spec.Name)
	}
	if !strings.HasPrefix(spec.URL, "http://127.0.0.1:") {
		t.Fatalf("url = %q, want a loopback address", spec.URL)
	}
	if spec.Type != "streamable-http" {
		t.Fatalf("type = %q", spec.Type)
	}
}

func closeProvider(t *testing.T, p capacp.SessionMCPProvider) {
	t.Helper()
	closer, ok := p.(interface{ Close() error })
	if !ok {
		t.Fatalf("provider %T does not expose Close", p)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestBearerAuthorization(t *testing.T) {
	t.Parallel()

	p, err := New(Config{APIToken: "s3cret"}, Deps{Provider: stubProvider(stubTool{"now"})})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProvider(t, p)

	servers, err := p.SessionMCPServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	url := servers[0].HTTP.URL
	if got := servers[0].HTTP.Headers["Authorization"]; got != "Bearer s3cret" {
		t.Fatalf("authorization header = %q", got)
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status without a token = %d, want 401", resp.StatusCode)
	}
}

func TestParseBindsValidates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   map[string]HeaderBind
		ok   bool
	}{
		{"empty", nil, true},
		{"header default", map[string]HeaderBind{"sessionID": {From: "ctx:session_id"}}, true},
		{"missing from", map[string]HeaderBind{"x": {}}, false},
		{"needs ctx prefix", map[string]HeaderBind{"x": {From: "session_id"}}, false},
		{"only header transport", map[string]HeaderBind{"x": {From: "ctx:session_id", In: "body"}}, false},
		{"duplicate header", map[string]HeaderBind{
			"a": {From: "ctx:session_id", Name: "S"},
			"b": {From: "ctx:user_id", Name: "S"},
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseBinds(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// ctx:route must round-trip the full RouteRef. Without it a tool that stores a
// destination (tool/schedule) persists a route with no target and can never
// deliver its result.
func TestContextFromHeadersRestoresRoute(t *testing.T) {
	t.Parallel()

	route := rctx.SessionRouteFromDelivery("lark", "lark:oc_demo:u:ou_owner", "").Stable()
	raw, err := json.Marshal(route)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Session-Key", "lark:oc_demo:u:ou_owner")
	req.Header.Set("Route-Key", string(raw))
	req.Header.Set("User-Key", "ou_owner")

	binds, err := parseBinds(map[string]HeaderBind{
		"sessionID": {From: "ctx:session_id", Name: "Session-Key"},
		"route":     {From: "ctx:route", Name: "Route-Key"},
		"userID":    {From: "ctx:user_id", Name: "User-Key"},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := contextFromInboundHeaders(req, binds, nil)
	env := rctx.EnvelopeFromContext(ctx)
	if env.Conversation != "lark:oc_demo:u:ou_owner" {
		t.Fatalf("conversation = %q", env.Conversation)
	}
	if env.Actor.UserID != "ou_owner" {
		t.Fatalf("user = %q", env.Actor.UserID)
	}
	delivery, ok := rctx.RouteSessionID(env.Route)
	if !ok || delivery != "lark:oc_demo:u:ou_owner" {
		t.Fatalf("route target lost across the MCP boundary: %+v (delivery %q)", env.Route, delivery)
	}
}

// A malformed ctx:route falls back to treating the value as a platform id, so a
// client that sends a bare platform still works.
func TestContextFromHeadersToleratesBadRoute(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Route-Key", "lark")

	binds, err := parseBinds(map[string]HeaderBind{"route": {From: "ctx:route", Name: "Route-Key"}})
	if err != nil {
		t.Fatal(err)
	}
	env := rctx.EnvelopeFromContext(contextFromInboundHeaders(req, binds, nil))
	if env.Route.Platform != "lark" {
		t.Fatalf("platform = %q, want the raw value to be used", env.Route.Platform)
	}
}

func TestRefreshPicksUpNewTools(t *testing.T) {
	t.Parallel()

	provider := &stubProviderImpl{tools: []agentkit.Tool{stubTool{"one"}}}
	p, err := New(Config{}, Deps{Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProvider(t, p)

	concrete := p.(*Provider)
	if err := concrete.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	provider.tools = []agentkit.Tool{stubTool{"one"}, stubTool{"two"}}
	if err := concrete.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	concrete.bridge.mu.Lock()
	names := make([]string, 0, len(concrete.bridge.tools))
	for name := range concrete.bridge.tools {
		names = append(names, name)
	}
	concrete.bridge.mu.Unlock()
	if len(names) != 2 {
		t.Fatalf("tools after refresh = %v, want both", names)
	}
}
