package mcp

import (
	"context"
	"os"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestResolveEnvValueURL(t *testing.T) {
	const key = "AGENTKIT_MCP_URL_TEST"
	t.Setenv(key, "https://mcp.example.com/mcp")

	got, err := resolveEnvValue(context.Background(), "env:"+key, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "https://mcp.example.com/mcp" {
		t.Fatalf("url = %q", got)
	}
}

func TestResolveStringMapHeaders(t *testing.T) {
	const key = "AGENTKIT_MCP_API_KEY_TEST"
	t.Setenv(key, "secret")

	got, err := resolveStringMap(context.Background(), map[string]string{
		"X-agenthub-apikey": "env:" + key,
		"Accept":            "application/json",
	}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got["X-agenthub-apikey"] != "secret" {
		t.Fatalf("api key = %q", got["X-agenthub-apikey"])
	}
	if got["Accept"] != "application/json" {
		t.Fatalf("accept = %q", got["Accept"])
	}
}

func TestMergeHeaderFuncStaticAndBind(t *testing.T) {
	t.Parallel()

	fn := mergeHeaderFunc(map[string]string{
		"X-agenthub-apikey": "static-key",
	}, []bindConfig{
		{Key: "X-User-Id", From: "ctx:user_id", In: "header"},
	})
	ctx := context.WithValue(context.Background(), agentkit.KeyUserID, "u-9")
	headers := fn(ctx)
	if headers["X-agenthub-apikey"] != "static-key" {
		t.Fatalf("static header = %q", headers["X-agenthub-apikey"])
	}
	if headers["X-User-Id"] != "u-9" {
		t.Fatalf("bind header = %q", headers["X-User-Id"])
	}
}

func TestResolveEnvValueMissingCredential(t *testing.T) {
	t.Parallel()

	missing := "AGENTKIT_MCP_MISSING_" + os.Getenv("USER")
	_, err := resolveEnvValue(context.Background(), "env:"+missing, nil)
	if err == nil {
		t.Fatal("expected error for missing credential")
	}
}
