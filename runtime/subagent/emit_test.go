package subagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lengzhao/agentkit"
)

func TestForwardParentEmitForwardsToolCallsOnly(t *testing.T) {
	t.Parallel()

	parentSession := agentkit.SessionID("chat-api:default_channel:t:conv_abc")
	ctx := context.Background()
	ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, parentSession)

	var got []agentkit.OutboundEvent
	parent := agentkit.OutboundEmit(func(_ context.Context, event agentkit.OutboundEvent) error {
		got = append(got, event)
		return nil
	})
	emit := forwardParentEmit(ctx, parent)

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
		SessionID: "sub:parent:researcher:1",
		AgentID:   "sub:researcher",
		Type:      agentkit.EventMessageUpdate,
		Data:      toolEnd,
	}); err != nil {
		t.Fatal(err)
	}

	resultData, _ := json.Marshal(agentkit.ToolResult{
		ID:      "call_1",
		Name:    "grep",
		Content: "matched 3 lines",
	})
	if err := emit(ctx, agentkit.OutboundEvent{
		SessionID: "sub:parent:researcher:1",
		AgentID:   "sub:researcher",
		Type:      agentkit.EventToolResult,
		Data:      resultData,
	}); err != nil {
		t.Fatal(err)
	}

	textDelta, _ := json.Marshal(agentkit.MessageUpdatePayload{
		AssistantMessageEvent: agentkit.AssistantMessageEvent{
			Type:  agentkit.AssistantEventTextDelta,
			Delta: "hidden",
		},
	})
	if err := emit(ctx, agentkit.OutboundEvent{
		SessionID: "sub:parent:researcher:1",
		Type:      agentkit.EventMessageUpdate,
		Data:      textDelta,
	}); err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("forwarded %d events, want 2 (tool call end + tool result)", len(got))
	}
	if got[0].SessionID != parentSession {
		t.Fatalf("session = %q, want parent delivery %q", got[0].SessionID, parentSession)
	}
	if got[0].Type != agentkit.EventMessageUpdate {
		t.Fatalf("first event type = %q, want message/update", got[0].Type)
	}
	var payload agentkit.MessageUpdatePayload
	if err := json.Unmarshal(got[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.AssistantMessageEvent.ToolName != "grep" {
		t.Fatalf("tool = %q", payload.AssistantMessageEvent.ToolName)
	}
	if got[1].Type != agentkit.EventToolResult {
		t.Fatalf("second event type = %q, want tool/result", got[1].Type)
	}
	if got[1].SessionID != parentSession {
		t.Fatalf("result session = %q, want parent delivery %q", got[1].SessionID, parentSession)
	}
}

func TestForwardParentEmitNilWithoutParentSession(t *testing.T) {
	t.Parallel()

	emit := forwardParentEmit(context.Background(), func(context.Context, agentkit.OutboundEvent) error {
		t.Fatal("parent emit should not be called")
		return nil
	})
	if emit != nil {
		t.Fatal("expected nil emit without parent session in context")
	}
}
