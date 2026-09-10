package acpclient

import (
	"testing"

	capacp "github.com/lengzhao/agentkit/cap/acp"
)

func TestFingerprintStableAcrossEnvOrder(t *testing.T) {
	a := []capacp.SessionMCPServer{{
		Stdio: &capacp.StdioMCPServer{
			Name:    "kit",
			Command: "tool-proxy",
			Args:    []string{"--session"},
			Env:     map[string]string{"B": "2", "A": "1"},
		},
	}}
	b := []capacp.SessionMCPServer{{
		Stdio: &capacp.StdioMCPServer{
			Name:    "kit",
			Command: "tool-proxy",
			Args:    []string{"--session"},
			Env:     map[string]string{"A": "1", "B": "2"},
		},
	}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("expected stable fingerprint")
	}
}

func TestToMCPServersSkipsEmpty(t *testing.T) {
	if got := ToMCPServers(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
	if got := ToMCPServers([]capacp.SessionMCPServer{{}}); got != nil {
		t.Fatalf("expected nil for missing stdio, got %v", got)
	}
}
