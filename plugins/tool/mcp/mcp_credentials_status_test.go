package mcp

import (
	"context"
	"strings"
	"testing"

)

func TestFormatMCPCredentialStatus(t *testing.T) {
	t.Parallel()
	servers := []serverConfig{{
		Name: "github",
		Env:  map[string]string{"TOKEN": "env:GITHUB_TOKEN"},
		URL:  "env:MCP_URL",
	}}
	creds := stubScopedCredentials{byScope: map[string]map[string]string{
		"mcp.github": {"GITHUB_TOKEN": "set"},
	}}
	out := formatMCPCredentialStatus(context.Background(), servers, creds)
	if !strings.Contains(out, "mcp.github:") {
		t.Fatalf("output=%q, want scope header", out)
	}
	if !strings.Contains(out, "GITHUB_TOKEN") || !strings.Contains(out, "ok") {
		t.Fatalf("output=%q, want GITHUB_TOKEN ok", out)
	}
	if !strings.Contains(out, "MCP_URL") || !strings.Contains(out, "missing") {
		t.Fatalf("output=%q, want MCP_URL missing", out)
	}
}
