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
	"time"

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

	store, err := plugincredentials.New(plugincredentials.Config{}, plugincredentials.EnvDeps{})
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

func TestLangfuseExporterPrefersFirstTextTimeForTTFT(t *testing.T) {
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

	store, err := plugincredentials.New(plugincredentials.Config{}, plugincredentials.EnvDeps{})
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

	toolStart := time.Date(2026, 9, 1, 8, 50, 55, 0, time.UTC)
	textStart := time.Date(2026, 9, 1, 8, 50, 52, 0, time.UTC)
	ctx, endTurn := exp.BeginTurn(context.Background(), captelemetry.TurnMeta{
		TurnID:    "turn-1",
		SessionID: "cli:default",
		Input:     "hello",
	})
	ctx, endGen := exp.BeginObservation(ctx, captelemetry.ObservationMeta{
		Name:  "llm.generation",
		Kind:  captelemetry.KindGeneration,
		Model: "gpt-5.4",
	})
	endGen(captelemetry.ObservationEnd{
		Output:              "answer",
		CompletionStartTime: toolStart,
		FirstTextTime:       textStart,
	})
	endTurn(captelemetry.TurnEnd{Output: "done"})
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(bodies)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "2026-09-01T08:50:52Z") {
		t.Fatalf("expected first text time in %s", text)
	}
	if strings.Contains(text, "2026-09-01T08:50:55Z") {
		t.Fatalf("tool start time should not be used when text exists: %s", text)
	}
}

func TestLangfuseExporterSendsCompletionStartTime(t *testing.T) {
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

	store, err := plugincredentials.New(plugincredentials.Config{}, plugincredentials.EnvDeps{})
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

	completionStart := time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)
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
	endGen(captelemetry.ObservationEnd{
		Output:              "answer",
		CompletionStartTime: completionStart,
	})
	endTurn(captelemetry.TurnEnd{Output: "done"})
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(bodies)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "completionStartTime") {
		t.Fatalf("expected completionStartTime in %s", raw)
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

	store, err := plugincredentials.New(plugincredentials.Config{}, plugincredentials.EnvDeps{})
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
		Name:      "llm.generation",
		Kind:      captelemetry.KindGeneration,
		Model:     "gpt-5.4",
		Input:     "prompt",
		ToolNames: []string{"read", "grep"},
	})
	endGen(captelemetry.ObservationEnd{
		Output: `{"content":"answer","role":"assistant"}`,
		Usage: &captelemetry.Usage{
			InputTokens:  12,
			OutputTokens: 4,
			TotalTokens:  16,
		},
	})
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
	for _, want := range []string{"trace-create", "generation-create", "generation-update", "span-create", "span-update", `"tools":"read,grep"`, `"tools":["read","grep"]`, `"input":12`, `"output":4`} {
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

	store, err := plugincredentials.New(plugincredentials.Config{}, plugincredentials.EnvDeps{})
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

func TestLangfuseExporterRecordsTurnUsageAndOutput(t *testing.T) {
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

	store, err := plugincredentials.New(plugincredentials.Config{}, plugincredentials.EnvDeps{})
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
		Input:     `{"content":"hello","role":"user"}`,
	})
	endTurn(captelemetry.TurnEnd{
		Output: "progress\n-------------\n[ask_user] pick one\noptions: a, b\n-------------\nfinal answer",
		Usage: &captelemetry.Usage{
			InputTokens:  20,
			OutputTokens: 8,
			TotalTokens:  28,
		},
	})
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	raw, err := json.Marshal(bodies)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"hello", "final answer", "usage_input_tokens", "usage_output_tokens", "usage_total_tokens"} {
		if !strings.Contains(text, want) {
			t.Fatalf("batch missing %q in %s", want, text)
		}
	}
}

func TestLangfuseMissingCredentialFailsAtBuild(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")

	store, err := plugincredentials.New(plugincredentials.Config{}, plugincredentials.EnvDeps{})
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
