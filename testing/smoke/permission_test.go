package smoke_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/tools"
	"github.com/lengzhao/agentkit/testing/agenttest"
)

// Smoke tests for policy DecisionAsk flowing through Loop permission broker:
// permission/request → user reply → permission/resolved → tool execution.

func TestSmokePermissionAskAllowViaLoop(t *testing.T) {
	t.Parallel()
	runPermissionLoopSmoke(t, permissionLoopCase{
		replyText:      "y",
		wantToolResult: "secret",
		wantDenied:     false,
	})
}

func TestSmokePermissionAskDenyViaLoop(t *testing.T) {
	t.Parallel()
	runPermissionLoopSmoke(t, permissionLoopCase{
		replyText:  "n",
		wantDenied: true,
	})
}

type permissionLoopCase struct {
	replyText      string
	wantToolResult string
	wantDenied     bool
}

func runPermissionLoopSmoke(t *testing.T, tc permissionLoopCase) {
	t.Helper()

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
		Policies: []agentkit.Policy{agenttest.AskAllToolsPolicy("confirm echo")},
	})
	ag, store := agenttest.NewScriptedAgent(t, agenttest.ScriptedAgentConfig{
		Steps: []llm.ScriptedStep{
			{
				ToolCalls: []agentkit.ToolCall{{
					ID: "call-echo", Name: "echo", Input: []byte(`{"text":"secret"}`),
				}},
			},
			{Text: "done"},
		},
		Tools: toolRT,
	})

	loopInst, err := loop.New(loop.Config{DefaultAgent: "smoke"}, loop.Deps{Agents: []agentkit.Agent{ag}})
	if err != nil {
		t.Fatal(err)
	}
	l := loopInst.(*loop.Default)

	sessionID := agentkit.SessionID("smoke:permission")
	ctx := context.Background()

	var outboundMu sync.Mutex
	var outbound []agentkit.OutboundEvent
	emit := func(ctx context.Context, out agentkit.OutboundEvent) error {
		outboundMu.Lock()
		outbound = append(outbound, out)
		outboundMu.Unlock()

		if out.Type != agentkit.EventPermissionRequest {
			return nil
		}
		var payload permission.RequestPayload
		if err := json.Unmarshal(out.Data, &payload); err != nil {
			t.Errorf("decode permission request: %v", err)
			return nil
		}
		go deliverPermissionReply(t, l, sessionID, payload.Request.ID, tc.replyText)
		return nil
	}

	if err := loopInst.Dispatch(ctx, agentkit.LoopRequest{
		Event: agentkit.MessageEvent{
			PlatformID: "cli",
			Message: agentkit.ModelMessage{
				Role:    "user",
				Content: []agentkit.ContentPart{{Type: "text", Text: "echo secret"}},
			},
			Envelope: agentkit.TurnEnvelope{Conversation: string(sessionID)},
		},
		Capability: permission.Capability{
			Interactive: true,
			AnswerScope: permission.ScopeAnyone,
		},
		Emit: emit,
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	outboundMu.Lock()
	defer outboundMu.Unlock()
	if got := countOutbound(outbound, agentkit.EventPermissionRequest); got != 1 {
		t.Fatalf("permission/request = %d, want 1", got)
	}
	if got := countOutbound(outbound, agentkit.EventPermissionResolved); got != 1 {
		t.Fatalf("permission/resolved = %d, want 1", got)
	}

	events := agenttest.SessionEvents(t, ctx, store, sessionID)
	if tc.wantDenied {
		agenttest.AssertNoToolResultWithContent(t, events, "call-echo", "secret")
		agenttest.AssertToolResultContains(t, events, "call-echo", "resolved")
	} else {
		agenttest.AssertToolResultContains(t, events, "call-echo", tc.wantToolResult)
	}
	agenttest.AssertEventAtLeast(t, events, agentkit.EventTurnEnd, 1)
}

func deliverPermissionReply(t *testing.T, l *loop.Default, sessionID agentkit.SessionID, requestID, text string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if l.TryDeliverPermission(agentkit.MessageEvent{
			Envelope: agentkit.TurnEnvelope{Conversation: string(sessionID)},
			Reply: permission.MarshalReply(permission.Reply{
				RequestID: requestID,
				Text:      text,
			}),
		}) {
			return
		}
		select {
		case <-deadline:
			t.Error("timed out delivering permission reply")
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func countOutbound(events []agentkit.OutboundEvent, typ agentkit.EventType) int {
	n := 0
	for _, ev := range events {
		if ev.Type == typ {
			n++
		}
	}
	return n
}
