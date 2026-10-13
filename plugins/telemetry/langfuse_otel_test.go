package telemetry_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	plugincredentials "github.com/lengzhao/agentkit/plugins/credentials"
	plugintelemetry "github.com/lengzhao/agentkit/plugins/telemetry"
)

type otlpRequest struct {
	path          string
	authorization string
	ingestVersion string
	body          []byte
}

func startOTLPServer(t *testing.T) (*httptest.Server, func() []otlpRequest) {
	t.Helper()
	var mu sync.Mutex
	var requests []otlpRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		mu.Lock()
		requests = append(requests, otlpRequest{
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			ingestVersion: r.Header.Get("x-langfuse-ingestion-version"),
			body:          body,
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, func() []otlpRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]otlpRequest(nil), requests...)
	}
}

func newV4Exporter(t *testing.T, baseURL string, cfg plugintelemetry.LangfuseConfig) captelemetry.Exporter {
	t.Helper()
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk-test")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk-test")
	store, err := plugincredentials.NewStatic(plugincredentials.Config{}, plugincredentials.EnvDeps{FS: testCredentialsFS(t)})
	if err != nil {
		t.Fatal(err)
	}
	cfg.BaseURL = baseURL
	cfg.PublicKeyRef = "env:LANGFUSE_PUBLIC_KEY"
	cfg.SecretKeyRef = "env:LANGFUSE_SECRET_KEY"
	cfg.FlushIntervalSeconds = 1
	exp, err := plugintelemetry.NewLangfuse(cfg, plugintelemetry.LangfuseDeps{Credentials: store, Telemetry: mustToolkit(t)})
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}
	return exp
}

// 默认（未配置 ingestVersion）走 v4 OTLP 接口 /api/public/otel/v1/traces。
func TestLangfuseV4DefaultsToOTLPIngestion(t *testing.T) {
	server, getRequests := startOTLPServer(t)
	exp := newV4Exporter(t, server.URL, plugintelemetry.LangfuseConfig{})

	turnID := uuid.NewString()
	ctx, endTurn := exp.BeginTurn(context.Background(), captelemetry.TurnMeta{
		TurnID:    turnID,
		SessionID: "cli:default",
		UserID:    "user-1",
		Model:     "gpt-5.4",
		Input:     "hello",
	})
	ctx, endGen := exp.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name:  "llm.generation",
		Kind:  captelemetry.KindGeneration,
		Model: "gpt-5.4",
		Input: "prompt",
	})
	endGen(captelemetry.ObservationEnd{
		Output: "answer",
		Usage:  &captelemetry.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
	})
	endTurn(captelemetry.TurnEnd{Output: "done"})
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	requests := getRequests()
	if len(requests) == 0 {
		t.Fatal("expected OTLP requests")
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("pk-test:sk-test"))
	var body []byte
	for _, req := range requests {
		if req.path != "/api/public/otel/v1/traces" {
			t.Fatalf("path = %q, want /api/public/otel/v1/traces", req.path)
		}
		if req.authorization != wantAuth {
			t.Fatalf("auth = %q, want %q", req.authorization, wantAuth)
		}
		if req.ingestVersion != "4" {
			t.Fatalf("x-langfuse-ingestion-version = %q, want 4", req.ingestVersion)
		}
		body = append(body, req.body...)
	}

	// OTLP protobuf payload embeds attribute strings and trace IDs in the clear.
	text := string(body)
	for _, want := range []string{
		"agent.turn",
		"llm.generation",
		"langfuse.observation.type",
		"generation",
		"langfuse.observation.model.name",
		"gpt-5.4",
		"langfuse.session.id",
		"cli:default",
		"langfuse.user.id",
		"user-1",
		"langfuse.observation.usage_details",
		"langfuse.observation.input",
		"langfuse.observation.output",
		"langfuse.trace.metadata.model",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("OTLP payload missing %q", want)
		}
	}
	// UUID turn IDs map directly onto the 16-byte OTel trace ID.
	parsed, err := uuid.Parse(turnID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, string(parsed[:])) {
		t.Fatal("OTLP payload missing turn-derived trace ID")
	}
}

// 非 UUID 的 turn ID 也必须产生稳定、合法的 OTel trace ID（哈希派生）。
func TestLangfuseV4HandlesNonUUIDTurnID(t *testing.T) {
	server, getRequests := startOTLPServer(t)
	exp := newV4Exporter(t, server.URL, plugintelemetry.LangfuseConfig{})

	ctx, endTurn := exp.BeginTurn(context.Background(), captelemetry.TurnMeta{
		TurnID:    "turn-1",
		SessionID: "cli:default",
		Input:     "hello",
	})
	_, endSpan := exp.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name: "tool.bash",
		Kind: captelemetry.KindTool,
	})
	endSpan(captelemetry.ObservationEnd{Output: "ok"})
	endTurn(captelemetry.TurnEnd{Output: "done"})
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	var body []byte
	for _, req := range getRequests() {
		body = append(body, req.body...)
	}
	if !strings.Contains(string(body), "tool.bash") {
		t.Fatal("OTLP payload missing tool span")
	}
}

func TestLangfuseRejectsUnknownIngestVersion(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk-test")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk-test")
	store, err := plugincredentials.NewStatic(plugincredentials.Config{}, plugincredentials.EnvDeps{FS: testCredentialsFS(t)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = plugintelemetry.NewLangfuse(plugintelemetry.LangfuseConfig{
		IngestVersion: "v2",
	}, plugintelemetry.LangfuseDeps{Credentials: store, Telemetry: mustToolkit(t)})
	if err == nil || !strings.Contains(err.Error(), "ingestVersion") {
		t.Fatalf("expected ingestVersion validation error, got %v", err)
	}
}
