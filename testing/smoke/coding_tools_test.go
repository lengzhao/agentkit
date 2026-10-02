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

// E2E-010: scripted turn can chain write + bash-style shell tool in one session.
func TestSmokeWriteThenBashToolChain(t *testing.T) {
	t.Parallel()

	writePack, err := fs.NewFSMemory(fs.FSMemoryConfig{
		Files: map[string]string{},
		Tools: []string{"write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	bash, err := agentkit.NewTool("bash", func(_ context.Context, in struct {
		Command string `json:"command"`
	}) (struct {
		Stdout string `json:"stdout"`
	}, error) {
		return struct {
			Stdout string `json:"stdout"`
		}{Stdout: "ran:" + in.Command}, nil
	}).Description("smoke bash stub").Build()
	if err != nil {
		t.Fatal(err)
	}

	toolRT := agenttest.ToolsRuntime(t, tools.RuntimeDeps{
		Tools:     []agentkit.Tool{bash},
		ToolPacks: []agentkit.ToolPack{writePack},
		Approval:  agenttest.AllowAll{},
	})
	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-write", Name: "write",
				Input: []byte(`{"path":"work/out.txt","content":"hello"}`),
			}}},
			{ToolCalls: []agentkit.ToolCall{{
				ID: "call-bash", Name: "bash",
				Input: []byte(`{"command":"cat work/out.txt"}`),
			}}},
			{Text: "写文件并执行命令完成。"},
		},
		Tools: toolRT,
	})

	sessionID := agentkit.SessionID("smoke:write-bash")
	ctx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, ctx, ag, "写文件再跑 bash")

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	agenttest.AssertToolResultContains(t, events, "call-write", "work/out.txt")
	agenttest.AssertToolResultContains(t, events, "call-bash", "ran:cat work/out.txt")
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
}
