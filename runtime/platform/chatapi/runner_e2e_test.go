package chatapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/command"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/prompt"
	"github.com/lengzhao/agentkit/runtime/runner"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
)

// E2E-110: HTTP POST /v1/chat-messages drives a full runner turn and streams SSE.
func TestE2EHTTPChatMessageAgentTurn(t *testing.T) {
	const wantReply = "chat-api e2e reply"

	dir := t.TempDir()
	ws := workspace.Static(dir)
	store, err := session.NewStore(session.StoreConfig{Dir: "sessions"}, session.StoreDeps{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}
	commands, err := command.NewFromProviders(command.Config{}, []agentkit.CommandProvider{
		store.(agentkit.CommandProvider),
	})
	if err != nil {
		t.Fatal(err)
	}

	platformInst, err := New(Config{Path: "/v1/"}, Deps{
		SessionStore: store,
		Commands:     commands,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	plat := platformInst.(*Platform)

	llmProvider, err := llm.NewScripted(llm.ScriptedConfig{
		Steps: []llm.ScriptedStep{{Text: wantReply}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := prompt.NewAssembler(prompt.AssemblerConfig{}, prompt.AssemblerDeps{})
	if err != nil {
		t.Fatal(err)
	}
	toolRT, err := tools.NewRuntime(tools.RuntimeConfig{}, tools.RuntimeDeps{Approval: allowAll{}})
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Config{ID: "coder", MaxSteps: 5}, agent.Deps{
		SessionStore: store,
		LLM:          llmProvider,
		Tools:        toolRT,
		Prompt:       assembler,
		Workspace:    ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	loopInst, err := loop.New(loop.Config{DefaultAgent: "coder"}, loop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}
	root, err := runner.New(runner.Config{}, runner.Deps{
		Platform:     platformInst,
		Loop:         loopInst,
		SessionStore: store,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() {
		runDone <- root.Run(ctx, nil)
	}()

	body, _ := json.Marshal(chatRequest{Query: "hello from integration"})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat-messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Chat-API-Channel", "default_channel")
	req.Header.Set("X-Chat-API-User", "demo")
	rec := httptest.NewRecorder()

	plat.handleChatMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "chat-api") || !strings.Contains(out, "e2e rep") {
		t.Fatalf("SSE body missing assistant reply %q: %s", wantReply, out)
	}
	if !strings.Contains(out, "message_end") {
		t.Fatalf("SSE body missing message_end: %s", out)
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("runner: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not stop")
	}
}

type allowAll struct{}

func (allowAll) Ask(context.Context, agentkit.ApprovalRequest) (agentkit.ApprovalDecision, error) {
	return agentkit.ApprovalDecision{Allowed: true}, nil
}
