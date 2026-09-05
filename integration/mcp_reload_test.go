//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/tools"
	mcpplugin "github.com/lengzhao/agentkit/plugins/tool/mcp"
	"github.com/lengzhao/agentkit/testing/agenttest"
	"github.com/lengzhao/agentkit/testing/mcptest"
)

// E2E-501: editing mcp.json on disk and running /mcp -u exposes new tools for agent turns.
func TestIntegrationMCPEndToEndHotReload(t *testing.T) {
	if testing.Short() {
		t.Skip("integration mcp reload")
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(configPath, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	provider, err := mcpplugin.NewMCP(mcpplugin.MCPConfig{Files: []string{configPath}}, mcpplugin.MCPDeps{
		Workspace: rtworkspace.Static(dir),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	toolsBefore, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list before reload: %v", err)
	}
	if len(toolsBefore) != 0 {
		t.Fatalf("tools before reload = %d, want 0", len(toolsBefore))
	}

	serverBin, mcpRoot := mcptest.ServerBinaryAndRoot(t)
	raw, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"filesystem": map[string]any{
				"command": serverBin,
				"args":    []string{mcpRoot},
				"prefix":  "fs__",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cp, ok := agentkit.ToolProvider(provider).(agentkit.CommandProvider)
	if !ok {
		t.Fatal("mcp provider does not implement CommandProvider")
	}
	cmd := cp.Commands()[0]
	reloaded, err := cmd.CommandExec(ctx, "-u")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !strings.Contains(reloaded, "tool(s)") {
		t.Fatalf("reload output = %q", reloaded)
	}

	toolsAfter, err := provider.ListTools(ctx)
	if err != nil {
		t.Fatalf("list after reload: %v", err)
	}
	if len(toolsAfter) != 2 {
		t.Fatalf("tools after reload = %d, want 2", len(toolsAfter))
	}

	writeName := mcptest.ToolBySuffix(t, toolsAfter, "write_file").Name()
	readName := mcptest.ToolBySuffix(t, toolsAfter, "read_text_file").Name()

	toolRT := agenttest.ToolsRuntime(t, tools.RuntimeDeps{
		DynamicTools: []agentkit.ToolProvider{provider},
		Approval:     agenttest.AllowAll{},
	})
	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{
			{
				ToolCalls: []agentkit.ToolCall{{
					ID: "call-write", Name: writeName,
					Input: []byte(`{"path":"reload.txt","content":"hot reload works"}`),
				}},
			},
			{
				ToolCalls: []agentkit.ToolCall{{
					ID: "call-read", Name: readName,
					Input: []byte(`{"path":"reload.txt"}`),
				}},
			},
			{Text: "热加载后 MCP 工具可用。"},
		},
		Tools: toolRT,
	})

	sessionID := agentkit.SessionID("it:mcp-reload")
	turnCtx := agenttest.TurnContext(sessionID, agentkit.AgentID("smoke"))
	agenttest.RunTurn(t, turnCtx, ag, "通过热加载后的 MCP 写读文件")

	written := filepath.Join(mcpRoot, "reload.txt")
	rawDisk, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("read file on disk: %v", err)
	}
	if string(rawDisk) != "hot reload works" {
		t.Fatalf("disk content = %q", string(rawDisk))
	}

	events := agenttest.SessionEvents(t, turnCtx, store, sessionID)
	agenttest.AssertToolResultContains(t, events, "call-read", "hot reload works")
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
}
