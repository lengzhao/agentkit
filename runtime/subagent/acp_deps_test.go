package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	capsubagent "github.com/lengzhao/agentkit/cap/subagent"
	_ "github.com/lengzhao/agentkit/plugins/agent/acpremote"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/pluginkit/build"
	"github.com/lengzhao/pluginkit/manager"
)

func TestACPAgentInjectedWithoutMCP(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`delegation:
  use: subagent/loop-agent
  config:
    freshSession: true
    timeoutSeconds: 10
    agents:
      - name: reviewer
        description: review code
        agent: remote
        async: false
  deps:
    sessionStore: store
    agents: [backend]
backend:
  use: agent/acp-remote
  config:
    id: remote
    command: [%q, "-test.run=^TestACPSubprocessPeer$"]
    env:
      AGENTKIT_ACP_SUBAGENT_TEST: "1"
    autoApprove: false
    pool:
      strategy: none
  deps:
    workspace: ws
    fs: fs
    sessionStore: store
    sessionEvents: events
    telemetry: tk
ws:
  use: workspace/default
  config:
    global: %q
    local: %q
fs:
  use: filesystem/local
  deps:
    workspace: ws
store:
  use: session/store
  config:
    dir: sessions
  deps:
    workspace: ws
events:
  use: session/events
tk:
  use: telemetry/toolkit
`, exe, t.TempDir(), t.TempDir())
	doc, err := manager.FromYAML([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	spawner, _, err := build.Build[capsubagent.Spawner](t.Context(), doc.ToGraph(), "delegation")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(loopParentCtx(), 10*time.Second)
	defer cancel()
	ctx = rctx.ContextWithOutboundEmit(ctx, func(context.Context, agentkit.OutboundEvent) error { return nil })
	var prior string
	for i := 0; i < 2; i++ {
		out, err := spawner.Run(ctx, capsubagent.Request{Agent: "reviewer", Task: "answer"})
		if err != nil || out.Summary != "ACP answer" || out.Session == "" || out.Session == prior {
			t.Fatalf("delegation: %+v %v", out, err)
		}
		prior = out.Session
	}
}

// A real stdio peer exercises the ACP client, history persistence and lifecycle
// without a network service, model credentials or MCP endpoint.
func TestACPSubprocessPeer(t *testing.T) {
	if os.Getenv("AGENTKIT_ACP_SUBAGENT_TEST") != "1" {
		return
	}
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if dec.Decode(&req) != nil {
			os.Exit(0)
		}
		if req.ID == nil {
			continue
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}, "authMethods": []any{}}
		case "session/new":
			var params struct {
				MCPServers []any `json:"mcpServers"`
			}
			json.Unmarshal(req.Params, &params)
			if len(params.MCPServers) != 0 {
				os.Exit(3)
			}
			result = map[string]any{"sessionId": "remote"}
		case "session/prompt":
			enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "remote", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "ACP answer"}}}})
			result = map[string]any{"stopReason": "end_turn"}
		default:
			result = map[string]any{}
		}
		enc.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
}
