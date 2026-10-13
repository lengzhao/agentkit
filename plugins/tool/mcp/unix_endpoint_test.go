package mcp

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestParseUnixURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		socket   string
		endpoint string
		wantErr  bool
	}{
		{raw: "unix:///tmp/mcp.sock", socket: "/tmp/mcp.sock", endpoint: "http://localhost/"},
		{raw: "unix:///tmp/mcp.sock:/mcp", socket: "/tmp/mcp.sock", endpoint: "http://localhost/mcp"},
		{raw: "unix:///tmp/mcp.sock:/mcp?x=1", socket: "/tmp/mcp.sock", endpoint: "http://localhost/mcp?x=1"},
		{raw: "unix:///tmp/foo:bar.sock:/mcp", socket: "/tmp/foo:bar.sock", endpoint: "http://localhost/mcp"},
		{raw: "unix:///tmp/mcp.sock:", socket: "/tmp/mcp.sock", endpoint: "http://localhost/"},
		{raw: "UNIX:///tmp/mcp.sock:/sse", socket: "/tmp/mcp.sock", endpoint: "http://localhost/sse"},
		{raw: "unix:/tmp/mcp.sock:/mcp", socket: "/tmp/mcp.sock", endpoint: "http://localhost/mcp"},
		{raw: "unix://localhost/tmp/mcp.sock", wantErr: true},
		{raw: "unix://", wantErr: true},
		{raw: "unix:relative.sock", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()
			socket, endpoint, err := parseUnixURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got socket=%q endpoint=%q", socket, endpoint)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if socket != tt.socket {
				t.Fatalf("socket = %q, want %q", socket, tt.socket)
			}
			if endpoint != tt.endpoint {
				t.Fatalf("endpoint = %q, want %q", endpoint, tt.endpoint)
			}
		})
	}
}

func TestPrepareEndpointHTTPUnchanged(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"http://127.0.0.1:8080/mcp",
		"https://mcp.example.com/sse",
	} {
		endpoint, client, err := prepareEndpoint(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if endpoint != raw {
			t.Fatalf("endpoint = %q, want %q", endpoint, raw)
		}
		if client != nil {
			t.Fatalf("%s: http(s) should keep the default client", raw)
		}
	}
}

func TestConnectUnixStreamable(t *testing.T) {
	sock := serveUnixSocket(t, func() http.Handler {
		s := server.NewMCPServer("unix-test", "1.0.0", server.WithToolCapabilities(true))
		s.AddTool(mcplib.NewTool("ping", mcplib.WithDescription("ping")), func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
			return mcplib.NewToolResultText("pong"), nil
		})
		mux := http.NewServeMux()
		mux.Handle("/mcp", server.NewStreamableHTTPServer(s))
		return mux
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := connectServer(ctx, serverConfig{
		Name: "local",
		URL:  "unix://" + sock + ":/mcp",
		Type: "streamable-http",
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	listed, err := client.ListTools(ctx, mcplib.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "ping" {
		t.Fatalf("tools = %+v", listed.Tools)
	}
	result, err := client.CallTool(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{Name: "ping"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	out := convertCallResult(result)
	if out.IsError || out.Content != "pong" {
		t.Fatalf("result = %+v", out)
	}
}

func TestConnectUnixSSE(t *testing.T) {
	sock := serveUnixSocket(t, func() http.Handler {
		s := server.NewMCPServer("unix-sse", "1.0.0", server.WithToolCapabilities(true))
		s.AddTool(mcplib.NewTool("echo", mcplib.WithDescription("echo")), func(_ context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
			text, err := req.RequireString("text")
			if err != nil {
				return mcplib.NewToolResultError(err.Error()), nil
			}
			return mcplib.NewToolResultText(text), nil
		})
		return server.NewSSEServer(s)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := connectServer(ctx, serverConfig{
		Name: "local",
		URL:  "unix://" + sock + ":/sse",
		Type: "sse",
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	result, err := client.CallTool(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Name:      "echo",
			Arguments: map[string]any{"text": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	out := convertCallResult(result)
	if out.IsError || out.Content != "hi" {
		t.Fatalf("result = %+v", out)
	}
}

func serveUnixSocket(t *testing.T, handler func() http.Handler) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets")
	}
	dir, err := os.MkdirTemp("/tmp", "akmcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "mcp.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler()}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return sock
}
