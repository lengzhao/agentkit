package mcp

import (
	"context"
	"os"
	"testing"
	"time"

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

func TestPoolKeyGlobalVsTenant(t *testing.T) {
	t.Parallel()

	globalServer := serverConfig{Name: "remote", Global: true}
	localServer := serverConfig{Name: "filesystem"}

	ctxA := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C001"))
	ctxB := context.WithValue(context.Background(), agentkit.KeySessionID, agentkit.SessionID("slack:C002"))

	globalA := poolKey(ctxA, globalServer)
	globalB := poolKey(ctxB, globalServer)
	if globalA != globalB {
		t.Fatalf("global pool keys differ: %q vs %q", globalA, globalB)
	}

	localA := poolKey(ctxA, localServer)
	localB := poolKey(ctxB, localServer)
	if localA == localB {
		t.Fatalf("local pool keys should differ by tenant: %q", localA)
	}
}

func TestClientPoolEvictIdle(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now := base
	pool := &clientPool{
		sessions: map[string]*serverSession{
			"stale": {lastUsed: base.Add(-10 * time.Minute)},
			"fresh": {lastUsed: base},
		},
		idleTTL: 5 * time.Minute,
		now:     func() time.Time { return now },
	}
	pool.evictIdle(now)
	if _, ok := pool.sessions["stale"]; ok {
		t.Fatal("stale session should be evicted")
	}
	if _, ok := pool.sessions["fresh"]; !ok {
		t.Fatal("fresh session should remain")
	}
}

func TestIdleTimeoutFromConfig(t *testing.T) {
	t.Parallel()

	if got := idleTimeoutFromConfig(nil); got != defaultMCPIdleTimeout {
		t.Fatalf("nil = %v, want default %v", got, defaultMCPIdleTimeout)
	}
	zero := 0
	if got := idleTimeoutFromConfig(&zero); got != 0 {
		t.Fatalf("zero = %v, want disabled", got)
	}
	custom := 120
	if got := idleTimeoutFromConfig(&custom); got != 2*time.Minute {
		t.Fatalf("custom = %v, want 2m", got)
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
