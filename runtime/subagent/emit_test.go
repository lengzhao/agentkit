package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
)

func TestForwardParentEmitForwardsProgressSignals(t *testing.T) {
	t.Parallel()

	parentSession := agentkit.SessionID("chat-api:default_channel:t:conv_abc")
	ctx := rctx.ContextWithDeliveryRoute(context.Background(), "chat-api", parentSession)

	var got []agentkit.OutboundEvent
	parent := agentkit.OutboundEmit(func(_ context.Context, event agentkit.OutboundEvent) error {
		got = append(got, event)
		return nil
	})
	emit, _ := forwardParentEmit(ctx, parent)

	toolStart, _ := json.Marshal(agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:     agentkit.AssistantEventToolCallStart,
			ID:       "call_1",
			ToolName: "grep",
		},
	})
	if err := emit(ctx, agentkit.OutboundEvent{
		Route:   rctx.SessionRoute("chat-api", "sub:parent:researcher:1"),
		AgentID: "sub:researcher",
		Type:    agentkit.EventMessageUpdate,
		Data:    toolStart,
	}); err != nil {
		t.Fatal(err)
	}

	toolEnd, _ := json.Marshal(agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:     agentkit.AssistantEventToolCallEnd,
			ID:       "call_1",
			ToolName: "grep",
			ToolCall: &agentkit.ToolCall{
				ID:    "call_1",
				Name:  "grep",
				Input: []byte(`{"pattern":"foo"}`),
			},
		},
	})
	if err := emit(ctx, agentkit.OutboundEvent{
		Route:   rctx.SessionRoute("chat-api", "sub:parent:researcher:1"),
		AgentID: "sub:researcher",
		Type:    agentkit.EventMessageUpdate,
		Data:    toolEnd,
	}); err != nil {
		t.Fatal(err)
	}

	resultData, _ := json.Marshal(agentkit.ToolResult{
		ID:      "call_1",
		Name:    "grep",
		Content: "matched 3 lines",
	})
	if err := emit(ctx, agentkit.OutboundEvent{
		Route:   rctx.SessionRoute("chat-api", "sub:parent:researcher:1"),
		AgentID: "sub:researcher",
		Type:    agentkit.EventToolResult,
		Data:    resultData,
	}); err != nil {
		t.Fatal(err)
	}

	thinkingDelta, _ := json.Marshal(agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:  agentkit.AssistantEventThinkingDelta,
			Delta: "scanning repo",
		},
	})
	if err := emit(ctx, agentkit.OutboundEvent{
		Route:   rctx.SessionRoute("chat-api", "sub:parent:researcher:1"),
		AgentID: "sub:researcher",
		Type:    agentkit.EventMessageUpdate,
		Data:    thinkingDelta,
	}); err != nil {
		t.Fatal(err)
	}

	textDelta, _ := json.Marshal(agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:  agentkit.AssistantEventTextDelta,
			Delta: "hidden body",
		},
	})
	if err := emit(ctx, agentkit.OutboundEvent{
		Route: rctx.SessionRoute("chat-api", "sub:parent:researcher:1"),
		Type:  agentkit.EventMessageUpdate,
		Data:  textDelta,
	}); err != nil {
		t.Fatal(err)
	}

	if len(got) != 5 {
		t.Fatalf("forwarded %d events, want 5 (tool start, tool end, tool result, thinking, text-as-thinking)", len(got))
	}
	if got[0].Type != agentkit.EventMessageUpdate {
		t.Fatalf("first event type = %q, want message/update", got[0].Type)
	}
	var startPayload agentkit.MessageUpdatePayload
	if err := json.Unmarshal(got[0].Data, &startPayload); err != nil {
		t.Fatal(err)
	}
	if startPayload.AssistantMessageEvent.Type != agentkit.AssistantEventToolCallStart {
		t.Fatalf("first update = %q, want toolcall_start", startPayload.AssistantMessageEvent.Type)
	}
	if rctx.OutboundRouteID(got[0]) != parentSession {
		t.Fatalf("route = %q, want parent delivery %q", rctx.OutboundRouteID(got[0]), parentSession)
	}
	if got[2].Type != agentkit.EventToolResult {
		t.Fatalf("third event type = %q, want tool/result", got[2].Type)
	}
	var thinkPayload agentkit.MessageUpdatePayload
	if err := json.Unmarshal(got[3].Data, &thinkPayload); err != nil {
		t.Fatal(err)
	}
	if thinkPayload.AssistantMessageEvent.Delta != "scanning repo" {
		t.Fatalf("thinking delta = %q", thinkPayload.AssistantMessageEvent.Delta)
	}
	var textAsThink agentkit.MessageUpdatePayload
	if err := json.Unmarshal(got[4].Data, &textAsThink); err != nil {
		t.Fatal(err)
	}
	if textAsThink.AssistantMessageEvent.Type != agentkit.AssistantEventThinkingDelta {
		t.Fatalf("text remapped type = %q, want thinking_delta", textAsThink.AssistantMessageEvent.Type)
	}
	if textAsThink.AssistantMessageEvent.Delta != "hidden body" {
		t.Fatalf("text-as-thinking delta = %q", textAsThink.AssistantMessageEvent.Delta)
	}
}

func TestForwardParentEmitCondensesThinking(t *testing.T) {
	t.Parallel()

	parentSession := agentkit.SessionID("feishu:default:chat")
	ctx := rctx.ContextWithDeliveryRoute(context.Background(), "feishu", parentSession)

	var gotThinking int
	parent := agentkit.OutboundEmit(func(_ context.Context, event agentkit.OutboundEvent) error {
		if event.Type != agentkit.EventMessageUpdate {
			return nil
		}
		var payload agentkit.MessageUpdatePayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return nil
		}
		if payload.AssistantMessageEvent.Type == agentkit.AssistantEventThinkingDelta {
			gotThinking += utf8RuneCount(payload.AssistantMessageEvent.Delta)
		}
		return nil
	})
	emit, _ := forwardParentEmit(ctx, parent)

	chunk := strings.Repeat("x", maxForwardedThinkingDeltaRunes+20)
	for i := 0; i < 20; i++ {
		data, _ := json.Marshal(agentkit.MessageUpdatePayload{
			AssistantMessageEvent: agentkit.AssistantMessageEvent{
				Type:  agentkit.AssistantEventThinkingDelta,
				Delta: chunk,
			},
		})
		_ = emit(ctx, agentkit.OutboundEvent{
			Route: rctx.SessionRoute("feishu", string(parentSession)),
			Type:  agentkit.EventMessageUpdate,
			Data:  data,
		})
	}
	if gotThinking > maxForwardedThinkingTotalRunes {
		t.Fatalf("forwarded thinking runes = %d, want <= %d", gotThinking, maxForwardedThinkingTotalRunes)
	}
	if gotThinking == 0 {
		t.Fatal("expected some thinking to be forwarded")
	}
}

func utf8RuneCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func TestEmitSubagentLifecycleUsesParentDeliverySession(t *testing.T) {
	t.Parallel()

	parentSession := agentkit.SessionID("feishu:default:chat")
	ctx := rctx.ContextWithDeliveryRoute(context.Background(), "feishu", parentSession)

	var got []agentkit.OutboundEvent
	var gotMu sync.Mutex
	ctx = context.WithValue(ctx, agentkit.KeyOutboundEmit, agentkit.OutboundEmit(func(_ context.Context, event agentkit.OutboundEvent) error {
		gotMu.Lock()
		got = append(got, event)
		gotMu.Unlock()
		return nil
	}))

	start := sessstore.SubagentStartData{Agent: "researcher", Session: "sub:1", Task: "survey"}
	emitSubagentLifecycle(ctx, "parent-agent", agentkit.EventSubagentStart, start)
	end := sessstore.SubagentEndData{Agent: "researcher", Session: "sub:1", Status: "completed", Summary: "done"}
	emitSubagentLifecycle(ctx, "parent-agent", agentkit.EventSubagentEnd, end)
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		gotMu.Lock()
		n := len(got)
		gotMu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	gotMu.Lock()
	n := len(got)
	gotMu.Unlock()
	if n != 2 {
		t.Fatalf("events = %d, want 2", n)
	}
	var sawStart, sawEnd bool
	for _, ev := range got {
		switch ev.Type {
		case agentkit.EventSubagentStart:
			sawStart = true
			if rctx.OutboundRouteID(ev) != parentSession {
				t.Fatalf("route = %q, want parent delivery %q", rctx.OutboundRouteID(ev), parentSession)
			}
		case agentkit.EventSubagentEnd:
			sawEnd = true
		default:
			t.Fatalf("unexpected event type %q", ev.Type)
		}
	}
	if !sawStart || !sawEnd {
		t.Fatalf("saw start=%v end=%v", sawStart, sawEnd)
	}
}

func TestForwardParentEmitAsyncDoesNotBlock(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	parentSession := agentkit.SessionID("feishu:default:chat")
	ctx := rctx.ContextWithDeliveryRoute(context.Background(), "feishu", parentSession)
	ctx = context.WithValue(ctx, agentkit.KeyAsyncSubagent, true)

	parent := agentkit.OutboundEmit(func(context.Context, agentkit.OutboundEvent) error {
		<-block
		return nil
	})
	emit, closeForward := forwardParentEmit(ctx, parent)
	if emit == nil {
		t.Fatal("expected emit")
	}

	start := time.Now()
	payload := rctx.MarshalOutboundData(agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:  agentkit.AssistantEventToolCallStart,
			ID:    "tc1",
			Delta: "x",
		},
	})
	if err := emit(ctx, agentkit.OutboundEvent{Type: agentkit.EventMessageUpdate, Data: payload}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("forward emit blocked %v", elapsed)
	}
	close(block)
	if closeForward != nil {
		closeForward()
	}
}

func TestForwardParentEmitNilWithoutParentSession(t *testing.T) {
	t.Parallel()

	emit, _ := forwardParentEmit(context.Background(), func(context.Context, agentkit.OutboundEvent) error {
		t.Fatal("parent emit should not be called")
		return nil
	})
	if emit != nil {
		t.Fatal("expected nil emit without parent session in context")
	}
}
