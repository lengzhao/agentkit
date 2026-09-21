//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/filesystem"
	plugincredentials "github.com/lengzhao/agentkit/plugins/credentials"
	plugintelemetry "github.com/lengzhao/agentkit/plugins/telemetry"
	rtfilesystem "github.com/lengzhao/agentkit/runtime/filesystem"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/rctx"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// testCredentialsFS builds an unrestricted filesystem/local over a temp dir for
// credentials/env (tests resolve process env only; the fs is never touched).
func testCredentialsFS(t *testing.T) filesystem.Service {
	t.Helper()
	fs, err := rtfilesystem.New(rtfilesystem.Config{Root: ".", Unrestricted: true}, rtfilesystem.Deps{Workspace: rtworkspace.Static(t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// E2E-402: Langfuse exporter flushes after a loop-driven agent turn.
func TestIntegrationLangfuseExporterOnAgentTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("integration langfuse")
	}

	var ingestions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/public/ingestion" {
			ingestions.Add(1)
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"successes":[],"errors":[]}`))
	}))
	defer server.Close()

	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk-test")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk-test")

	creds, err := plugincredentials.NewStatic(plugincredentials.Config{}, plugincredentials.EnvDeps{FS: testCredentialsFS(t)})
	if err != nil {
		t.Fatal(err)
	}
	exp, err := plugintelemetry.NewLangfuse(plugintelemetry.LangfuseConfig{
		BaseURL:              server.URL,
		PublicKeyRef:         "env:LANGFUSE_PUBLIC_KEY",
		SecretKeyRef:         "env:LANGFUSE_SECRET_KEY",
		FlushIntervalSeconds: 1,
	}, plugintelemetry.LangfuseDeps{Credentials: creds, Telemetry: integrationTelemetry(t)})
	if err != nil {
		t.Fatal(err)
	}

	ag, _ := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{{Text: "langfuse e2e reply"}},
	})
	loopInst, err := loop.New(loop.Config{DefaultAgent: "smoke"}, loop.Deps{
		Agents:    []agentkit.Agent{ag},
		Telemetry: exp,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := loopInst.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{
			PlatformID: "cli",
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "hello langfuse"}},
			},
			Envelope: agentkit.TurnEnvelope{
				Conversation: "cli:langfuse-e2e",
				Workspace:    "cli:langfuse-e2e",
				Route:        rctx.SessionRoute("cli", "cli:langfuse-e2e"),
			},
		},
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if ingestions.Load() < 1 {
		t.Fatalf("ingestion calls = %d, want at least 1", ingestions.Load())
	}
}
