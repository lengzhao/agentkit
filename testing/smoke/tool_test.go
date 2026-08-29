package smoke_test

import (
	"context"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/plugins/tool/fs"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestSmokeMultiToolTurnDerivesCleanHistory(t *testing.T) {
	t.Parallel()

	readPack, err := fs.NewFSMemory(fs.FSMemoryConfig{
		Files: map[string]string{"README.md": "hello agentkit"},
		Tools: []string{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	toolRT := agenttest.ToolsRuntime(t, tools.RuntimeDeps{
		ToolPacks: []agentkit.ToolPack{readPack},
		Approval:  agenttest.AllowAll{},
	})
	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{
			{
				ToolCalls: []agentkit.ToolCall{{
					ID: "call-read", Name: "read", Input: []byte(`{"path":"README.md"}`),
				}},
			},
			{Text: "已读取 README。"},
		},
		Tools: toolRT,
	})

	sessionID := agentkit.SessionID("smoke:multi-tool")
	ctx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, ctx, ag, "读取 README")

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	agenttest.AssertToolResultContains(t, events, "call-read", "hello agentkit")
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)

	sess, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	agenttest.AssertDeriveMessagesToolCallsAnswered(t, sess, ctx)
}

func TestSmokePolicyDeniesToolExecution(t *testing.T) {
	t.Parallel()

	echo, err := agentkit.NewTool("echo", func(_ context.Context, in struct {
		Text string `json:"text"`
	}) (struct {
		Text string `json:"text"`
	}, error) {
		return struct {
			Text string `json:"text"`
		}{Text: in.Text}, nil
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	toolRT := agenttest.ToolsRuntime(t, tools.RuntimeDeps{
		Tools:    []agentkit.Tool{echo},
		Policies: []agentkit.Policy{agenttest.DenyAllToolsPolicy("blocked by smoke policy")},
		Approval: agenttest.AllowAll{},
	})
	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{
			{
				ToolCalls: []agentkit.ToolCall{{
					ID: "call-echo", Name: "echo", Input: []byte(`{"text":"secret"}`),
				}},
			},
			{Text: "工具被拒绝，改用文本回复。"},
		},
		Tools: toolRT,
	})

	sessionID := agentkit.SessionID("smoke:policy-deny")
	ctx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, ctx, ag, "echo 一下")

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	agenttest.AssertToolResultContains(t, events, "call-echo", "blocked by smoke policy")
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
}
