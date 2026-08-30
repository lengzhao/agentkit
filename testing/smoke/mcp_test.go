package smoke_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/agentkit/testing/mcptest"
)

// E2E-500: MCP stdio server tools mount on tools/runtime and run through a full agent turn.
func TestSmokeMCPDynamicToolsAgentTurn(t *testing.T) {
	provider, mcpRoot := mcptest.NewProvider(t)
	ctx := context.Background()

	toolsList, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(toolsList) != 2 {
		t.Fatalf("tools = %d, want 2", len(toolsList))
	}

	writeName := mcptest.ToolBySuffix(t, toolsList, "write_file").Name()
	readName := mcptest.ToolBySuffix(t, toolsList, "read_text_file").Name()

	toolRT := agenttest.ToolsRuntime(t, tools.RuntimeDeps{
		DynamicTools: []agentkit.ToolProvider{provider},
		Approval:     agenttest.AllowAll{},
	})
	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{
			{
				ToolCalls: []agentkit.ToolCall{{
					ID: "call-write", Name: writeName,
					Input: []byte(`{"path":"hello.txt","content":"from mcp smoke"}`),
				}},
			},
			{
				ToolCalls: []agentkit.ToolCall{{
					ID: "call-read", Name: readName,
					Input: []byte(`{"path":"hello.txt"}`),
				}},
			},
			{Text: "MCP 读写完成。"},
		},
		Tools: toolRT,
	})

	sessionID := agentkit.SessionID("smoke:mcp")
	turnCtx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, turnCtx, ag, "通过 MCP 写文件再读回来")

	written := filepath.Join(mcpRoot, "hello.txt")
	raw, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("read mcp root file: %v", err)
	}
	if string(raw) != "from mcp smoke" {
		t.Fatalf("disk content = %q", string(raw))
	}

	events := agenttest.SessionEvents(t, turnCtx, store, sessionID)
	agenttest.AssertToolResultContains(t, events, "call-read", "from mcp smoke")
	if got := agenttest.CountEvents(events, agentkit.EventToolResult); got < 2 {
		t.Fatalf("tool/result = %d, want at least 2", got)
	}
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)

	names := make([]string, 0, len(toolsList))
	for _, tool := range toolsList {
		names = append(names, tool.Name())
	}
	if !strings.HasPrefix(writeName, "fs__") {
		t.Fatalf("write tool = %q, want fs__ prefix", writeName)
	}
	_ = readName
}
