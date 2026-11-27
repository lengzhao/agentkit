package readoutput

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	rtworkspace "github.com/lengzhao/agentkit/runtime/workspace"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

func TestToolReadsOnlyCurrentConversation(t *testing.T) {
	ws := rtworkspace.Static(t.TempDir())
	ctx := rctx.WithWorkspaceService(agenttest.TurnContext("conversation:a", "agent"), ws)
	full := strings.Repeat("hello 中文", 2000)
	if _, err := derive.PrepareToolResultForStorage(ctx, "conversation:a", agentkit.ToolResult{ID: "call-1", Content: full}, 8192); err != nil {
		t.Fatal(err)
	}
	tool, err := New(Config{}, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	var page derive.ToolOutputPage
	raw := agenttest.CallTool(t, ctx, tool, `{"tool_call_id":"call-1"}`)
	if err := json.Unmarshal([]byte(raw), &page); err != nil {
		t.Fatal(err)
	}
	if page.Text == "" || page.NextOffset == nil || !strings.HasPrefix(full, page.Text) {
		t.Fatalf("page=%+v", page)
	}
}
