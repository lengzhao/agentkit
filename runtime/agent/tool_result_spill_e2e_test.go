package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/agentkit/runtime/tools"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

type bigOutputTool struct {
	name string
	body string
}

func (bigOutputTool) Name() string        { return "big" }
func (bigOutputTool) Description() string  { return "returns a large string" }
func (bigOutputTool) InputSchema() agentkit.JSONSchema {
	return agentkit.JSONSchema{Type: "object"}
}
func (t bigOutputTool) Call(_ context.Context, _ json.RawMessage) (string, error) {
	return t.body, nil
}

func TestRunTurnSpillsLargeToolResultToWorkspace(t *testing.T) {
	t.Parallel()

	full := strings.Repeat("line\n", 3000)
	store, wsRoot := agenttest.TempFileStore(t)
	ws := rtworkspace.Static(wsRoot)

	ag, _ := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		AgentID:   "spill",
		MaxSteps:  3,
		Store:     store,
		Workspace: ws,
		Steps: []llm.ScriptedStep{
			{
				ToolCalls: []agentkit.ToolCall{{
					ID:    "call-big",
					Name:  "big",
					Input: json.RawMessage(`{}`),
				}},
			},
			{Text: "done"},
		},
		Tools: agenttest.ToolsRuntime(t, tools.RuntimeDeps{
			Tools:    []agentkit.Tool{bigOutputTool{name: "big", body: full}},
			Approval: agenttest.AllowAll{},
		}),
	})

	sessionID := agentkit.SessionID("spill-e2e")
	ctx := session.ApplyEnvelopeToContext(context.Background(), agentkit.TurnEnvelope{
		Conversation: string(sessionID),
		Workspace:    string(sessionID),
	})
	if err := ag.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "run big"}},
		},
	}); err != nil {
		t.Fatalf("run turn: %v", err)
	}

	var spillRel string
	for _, ev := range agenttest.SessionEvents(t, ctx, store, sessionID) {
		if ev.Type != agentkit.EventToolResult {
			continue
		}
		var result agentkit.ToolResult
		if err := json.Unmarshal(ev.Data, &result); err != nil {
			t.Fatal(err)
		}
		if result.ID != "call-big" {
			continue
		}
		spillRel = result.Audit[session.AuditSpillPath]
		if spillRel == "" {
			t.Fatalf("tool result missing spill_path audit: %s", string(ev.Data))
		}
		if len(result.Content) >= len(full) {
			t.Fatalf("expected truncated session view, content len=%d", len(result.Content))
		}
		if !strings.Contains(result.Content, "Full output: "+spillRel) {
			t.Fatalf("content missing spill hint: %q", result.Content)
		}
	}
	if spillRel == "" {
		t.Fatal("no tool/result for call-big")
	}

	abs, err := session.SpillPathAbs(ctx, ws, spillRel)
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != full {
		t.Fatalf("spill file bytes=%d want=%d", len(onDisk), len(full))
	}
}
