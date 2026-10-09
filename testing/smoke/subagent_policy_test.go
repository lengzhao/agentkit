package smoke_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

const researcherFinishOnlyDef = `---
name: researcher
description: finish-only child for allowlist smoke
tools: [finish]
---
You are the research subagent for smoke tests.
`

// E2E-030: child definition allowlist blocks tools that exist on the child runtime.
func TestSmokeSubagentToolAllowlistDeniesWrite(t *testing.T) {
	t.Parallel()

	writePack, err := fs.NewFSMemory(fs.FSMemoryConfig{
		Files: map[string]string{"note.txt": "keep"},
		Tools: []string{"write"},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := subagentDelegateConfig()
	cfg.ResearcherDef = researcherFinishOnlyDef
	cfg.ChildToolPacks = []agentkit.ToolPack{writePack}
	cfg.Steps = []llm.ScriptedStep{
		{
			Text: "交给 researcher。",
			ToolCalls: []agentkit.ToolCall{{
				ID: "call-delegate", Name: "delegate",
				Input: []byte(`{"agent":"researcher","task":"尝试写文件"}`),
			}},
		},
		{ToolCalls: []agentkit.ToolCall{{
			ID: "call-write", Name: "write",
			Input: []byte(`{"path":"out.txt","content":"nope"}`),
		}}},
		{ToolCalls: []agentkit.ToolCall{{
			ID: "call-finish", Name: "finish",
			Input: []byte(`{"status":"completed","summary":"write blocked"}`),
		}}},
		{Text: "子 turn 收尾。"},
		{Text: "researcher 结论：write blocked。"},
	}

	env := agenttest.NewSubagentDelegateEnv(t, cfg)
	ctx := agenttest.TurnContext(env.LogicalID, agentkit.AgentID("nex"))
	agenttest.RunTurn(t, ctx, env.Agent, "委派只读子 agent")

	childID := agenttest.FindChildSessionID(t, agenttest.SessionEvents(t, ctx, env.Store, env.LogicalID))
	childEvents := agenttest.SessionEvents(t, ctx, env.Store, childID)
	agenttest.AssertToolResultContains(t, childEvents, "call-write", "tool not available to this subagent")
	if agenttest.CountEvents(childEvents, agentkit.EventToolResult) < 2 {
		t.Fatalf("child tool results = %d, want write deny + finish", agenttest.CountEvents(childEvents, agentkit.EventToolResult))
	}
}

// E2E-031: delegation wall-clock timeout ends with subagent/end error (deadline/canceled).
func TestSmokeSubagentDelegationTimesOut(t *testing.T) {
	t.Parallel()

	slow, err := agentkit.NewTool("slow", func(ctx context.Context, _ struct{}) (struct{}, error) {
		select {
		case <-ctx.Done():
			return struct{}{}, ctx.Err()
		case <-time.After(30 * time.Second):
			return struct{}{}, nil
		}
	}).Description("blocks until cancelled").Build()
	if err != nil {
		t.Fatal(err)
	}

	cfg := subagentDelegateConfig()
	cfg.ResearcherDef = `---
name: researcher
description: slow child for timeout smoke
tools: [slow, finish]
---
You are the research subagent for smoke tests.
`
	cfg.ChildTools = []agentkit.Tool{slow}
	cfg.Steps = []llm.ScriptedStep{
		{
			Text: "交给 researcher。",
			ToolCalls: []agentkit.ToolCall{{
				ID: "call-delegate", Name: "delegate",
				Input: []byte(`{"agent":"researcher","task":"run slow tool","timeoutSeconds":1}`),
			}},
		},
		{ToolCalls: []agentkit.ToolCall{{ID: "call-slow", Name: "slow", Input: []byte(`{}`)}}},
	}

	env := agenttest.NewSubagentDelegateEnv(t, cfg)
	ctx := agenttest.TurnContext(env.LogicalID, agentkit.AgentID("nex"))
	runErr := env.Agent.RunTurn(ctx, agentkit.TurnInput{
		Message: agentkit.ModelMessage{
			Role:    "user",
			Content: []agentkit.ContentPart{{Type: "text", Text: "委派会超时"}},
		},
	})

	events := agenttest.SessionEvents(t, ctx, env.Store, env.LogicalID)
	if got := agenttest.CountEvents(events, agentkit.EventSubagentEnd); got != 1 {
		t.Fatalf("subagent/end = %d, want 1", got)
	}
	var endPayload struct {
		Error string `json:"error"`
	}
	for _, ev := range events {
		if ev.Type != agentkit.EventSubagentEnd {
			continue
		}
		if err := json.Unmarshal(ev.Data, &endPayload); err != nil {
			t.Fatal(err)
		}
		break
	}
	if endPayload.Error == "" {
		t.Fatalf("subagent/end must carry error on timeout, data=%s", string(events[len(events)-1].Data))
	}
	if !strings.Contains(strings.ToLower(endPayload.Error), "deadline") &&
		!strings.Contains(strings.ToLower(endPayload.Error), "canceled") {
		t.Fatalf("subagent/end error = %q, want deadline or canceled", endPayload.Error)
	}
	if runErr == nil {
		t.Fatal("parent turn should fail when delegate times out")
	}
}
