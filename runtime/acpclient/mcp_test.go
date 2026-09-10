package acpclient

import (
	"testing"

	capacp "github.com/lengzhao/agentkit/cap/acp"
)

func TestToMCPServersHTTPAndSSE(t *testing.T) {
	specs := []capacp.SessionMCPServer{
		{HTTP: &capacp.HTTPMCPServer{
			Name:    "api",
			URL:     "https://mcp.example/http",
			Type:    "http",
			Headers: map[string]string{"Authorization": "Bearer x"},
		}},
		{SSE: &capacp.SSEMCPServer{
			Name: "events",
			URL:  "https://mcp.example/sse",
		}},
	}
	got := ToMCPServers(specs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Http == nil || got[0].Http.Url != "https://mcp.example/http" {
		t.Fatalf("http server: %+v", got[0])
	}
	if got[1].Sse == nil || got[1].Sse.Url != "https://mcp.example/sse" {
		t.Fatalf("sse server: %+v", got[1])
	}
}

func TestToMCPServersSkipsEmpty(t *testing.T) {
	if got := ToMCPServers(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
	if got := ToMCPServers([]capacp.SessionMCPServer{{}}); got != nil {
		t.Fatalf("expected nil for missing transport, got %v", got)
	}
}
