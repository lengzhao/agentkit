package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestMCPToolCallErrorContract(t *testing.T) {
	s := server.NewMCPServer("error-contract", "1.0.0", server.WithToolCapabilities(true))
	for name, result := range map[string]*mcplib.CallToolResult{
		"rejected": mcplib.NewToolResultError("fixture backend rejected"),
		"empty":    mcplib.NewToolResultError(""),
		"success":  mcplib.NewToolResultText("Error rate is zero"),
	} {
		s.AddTool(mcplib.NewTool(name), func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
			return result, nil
		})
	}
	httpServer := httptest.NewServer(server.NewStreamableHTTPServer(s))
	defer httpServer.Close()
	pool := newClientPool(0)
	p := &mcpProvider{loaded: true, pool: pool, servers: []serverConfig{
		{Name: "fixture", Prefix: "fixture__", Type: "streamable-http", URL: httpServer.URL},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer pool.evict(pool.poolKey(ctx, p.servers[0]))
	for _, tt := range []struct {
		name       string
		content    string
		diagnostic string
		wantErr    bool
	}{
		{name: "unknown", diagnostic: "unknown mcp tool", wantErr: true},
		{name: "rejected", content: "fixture backend rejected", diagnostic: "fixture backend rejected", wantErr: true},
		{name: "empty", diagnostic: "without diagnostic content", wantErr: true},
		{name: "success", content: "Error rate is zero"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			name := "fixture__" + tt.name
			if tt.name == "unknown" {
				name = "unknown__tool"
			}
			tool := &mcpTool{provider: p, def: toolDefinition{ExposedName: name}}
			out, err := tool.Call(ctx, json.RawMessage(`{}`))
			if (err != nil) != tt.wantErr {
				t.Errorf("Call error = %v, wantErr %v (content %q)", err, tt.wantErr, out)
			}
			if tt.name == "unknown" {
				if !strings.Contains(out, tt.diagnostic) {
					t.Errorf("diagnostic lost: %q", out)
				}
			} else if out != tt.content {
				t.Errorf("content = %q, want %q", out, tt.content)
			}
			if err != nil && !strings.Contains(err.Error(), tt.diagnostic) {
				t.Errorf("error = %v, want diagnostic %q", err, tt.diagnostic)
			}
		})
	}
}
