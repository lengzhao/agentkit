package telemetry_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	plugincredentials "github.com/lengzhao/agentkit/plugins/credentials"
	plugintelemetry "github.com/lengzhao/agentkit/plugins/telemetry"
)

func TestLangfuseExporterFlushUsesOTLP(t *testing.T) {

	var gotAuth string
	var gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/api/public/otel") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("x-langfuse-ingestion-version")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk-test")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk-test")

	store, err := plugincredentials.New(plugincredentials.Config{})
	if err != nil {
		t.Fatal(err)
	}

	exp, err := plugintelemetry.NewLangfuse(plugintelemetry.LangfuseConfig{
		BaseURL:              server.URL,
		PublicKeyRef:         "env:LANGFUSE_PUBLIC_KEY",
		SecretKeyRef:         "env:LANGFUSE_SECRET_KEY",
		FlushIntervalSeconds: 1,
	}, plugintelemetry.LangfuseDeps{Credentials: store})
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}

	ctx, endTurn := exp.BeginTurn(context.Background(), captelemetry.TurnMeta{
		TurnID:    "turn-1",
		SessionID: "cli:default",
		Input:     "hello",
	})
	endTurn(captelemetry.TurnEnd{})
	if err := exp.Flush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("pk-test:sk-test"))
	if gotAuth != wantAuth {
		t.Fatalf("auth = %q, want %q", gotAuth, wantAuth)
	}
	if gotVersion != "4" {
		t.Fatalf("ingestion version = %q", gotVersion)
	}
}

func TestLangfuseMissingCredentialFailsAtBuild(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")

	store, err := plugincredentials.New(plugincredentials.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = plugintelemetry.NewLangfuse(plugintelemetry.LangfuseConfig{
		PublicKeyRef: "env:LANGFUSE_PUBLIC_KEY",
		SecretKeyRef: "env:LANGFUSE_SECRET_KEY",
	}, plugintelemetry.LangfuseDeps{Credentials: store})
	if err == nil || !strings.Contains(err.Error(), "LANGFUSE_PUBLIC_KEY") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}
