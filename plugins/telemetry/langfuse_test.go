package telemetry_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	plugincredentials "github.com/lengzhao/agentkit/plugins/credentials"
	plugintelemetry "github.com/lengzhao/agentkit/plugins/telemetry"
)

func TestLangfuseExporterFlushUsesIngestionAPI(t *testing.T) {
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"successes":[],"errors":[]}`))
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
	endTurn(captelemetry.TurnEnd{Output: "hi"})
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if gotPath != "/api/public/ingestion" {
		t.Fatalf("path = %q, want /api/public/ingestion", gotPath)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("pk-test:sk-test"))
	if gotAuth != wantAuth {
		t.Fatalf("auth = %q, want %q", gotAuth, wantAuth)
	}
}

func TestLangfuseExporterSendsGenerationAndTool(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, payload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"successes":[],"errors":[]}`))
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
		t.Fatal(err)
	}

	ctx, endTurn := exp.BeginTurn(context.Background(), captelemetry.TurnMeta{
		TurnID:    "turn-1",
		SessionID: "cli:default",
		Input:     "hello",
	})
	ctx, endGen := exp.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name:  "llm.generation",
		Kind:  captelemetry.KindGeneration,
		Model: "gpt-5.4",
		Input: "prompt",
	})
	endGen(captelemetry.ObservationEnd{Output: "answer"})
	ctx, endTool := exp.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name:  "tool.bash",
		Kind:  captelemetry.KindTool,
		Input: `{"cmd":"ls"}`,
	})
	endTool(captelemetry.ObservationEnd{Output: "ok"})
	endTurn(captelemetry.TurnEnd{Output: "done"})
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if len(bodies) == 0 {
		t.Fatal("expected ingestion requests")
	}
	raw, err := json.Marshal(bodies)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"trace-create", "generation-create", "generation-update", "span-create", "span-update"} {
		if !strings.Contains(text, want) {
			t.Fatalf("batch missing %q in %s", want, text)
		}
	}
	if !strings.Contains(text, "parentObservationId") {
		t.Fatalf("expected tool span parentObservationId in %s", text)
	}
}

func TestLangfuseExporterNestsSubagentGeneration(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, payload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"successes":[],"errors":[]}`))
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
		t.Fatal(err)
	}

	ctx, endTurn := exp.BeginTurn(context.Background(), captelemetry.TurnMeta{
		TurnID:    "turn-1",
		SessionID: "cli:default",
		AgentID:   "coder",
	})
	ctx, endParentGen := exp.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name: "llm.generation",
		Kind: captelemetry.KindGeneration,
	})
	endParentGen(captelemetry.ObservationEnd{Output: "delegate"})
	ctx, endDelegate := exp.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name: "tool.delegate",
		Kind: captelemetry.KindTool,
	})
	ctx, endSubagent := exp.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name:    "subagent.researcher",
		Kind:    captelemetry.KindSpan,
		AgentID: "sub:researcher",
		Scope:   true,
	})
	ctx, endChildGen := exp.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name:    "llm.generation",
		Kind:    captelemetry.KindGeneration,
		AgentID: "sub:researcher",
	})
	endChildGen(captelemetry.ObservationEnd{Output: "answer"})
	endSubagent(captelemetry.ObservationEnd{Output: "answer"})
	endDelegate(captelemetry.ObservationEnd{Output: "answer"})
	endTurn(captelemetry.TurnEnd{})
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(bodies)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"subagent.researcher", `"agent_id":"sub:researcher"`, "parentObservationId"} {
		if !strings.Contains(text, want) {
			t.Fatalf("batch missing %q in %s", want, text)
		}
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
