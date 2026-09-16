package acpclient

import (
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
)

func TestMCPServerNames(t *testing.T) {
	names := MCPServerNames([]acpsdk.McpServer{
		{Stdio: &acpsdk.McpServerStdio{Name: "b"}},
		{Http: &acpsdk.McpServerHttpInline{Name: "a", Url: "http://localhost/mcp"}},
	})
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("names = %v", names)
	}
}
