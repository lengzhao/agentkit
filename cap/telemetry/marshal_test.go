package telemetry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/cap/telemetry"
)

func TestFormatMessageEncodesRoleAndContent(t *testing.T) {
	t.Parallel()

	raw := telemetry.FormatMessage(agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}},
	})
	if raw != `{"content":"hello","role":"user"}` {
		t.Fatalf("FormatMessage = %q", raw)
	}
}

func TestToolNamesFromSpecs(t *testing.T) {
	t.Parallel()

	names := telemetry.ToolNamesFromSpecs([]agentkit.ToolSpec{
		{Name: "read"},
		{Name: "grep"},
	})
	if len(names) != 2 || names[0] != "read" || names[1] != "grep" {
		t.Fatalf("ToolNamesFromSpecs = %#v", names)
	}
}

func TestTurnAccumRecordsOutputAndUsage(t *testing.T) {
	t.Parallel()

	ctx := telemetry.WithTurnAccum(context.Background())
	telemetry.RecordTurnOutput(ctx, agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "done"}},
	})
	telemetry.RecordTurnUsage(ctx, telemetry.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	telemetry.RecordTurnUsage(ctx, telemetry.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5})

	end := telemetry.TurnEndFromAccum(ctx)
	if end.Output != "done" {
		t.Fatalf("output = %q", end.Output)
	}
	if end.Usage == nil || end.Usage.InputTokens != 13 || end.Usage.OutputTokens != 7 || end.Usage.TotalTokens != 20 {
		t.Fatalf("usage = %#v", end.Usage)
	}
}

func TestWrapOutboundEmitRecordsFinalSendAndAskUser(t *testing.T) {
	t.Parallel()

	ctx := telemetry.WithTurnAccum(context.Background())
	emit := telemetry.WrapOutboundEmit(ctx, func(_ context.Context, _ agentkit.OutboundEvent) error { return nil })

	toolStep, _ := json.Marshal(agentkit.MessageEndPayload{
		Message: agentkit.ModelMessage{
			Role:      "assistant",
			Content:   []agentkit.ContentPart{{Type: "text", Text: "internal"}},
			ToolCalls: []agentkit.ToolCall{{ID: "1", Name: "read"}},
		},
	})
	if err := emit(ctx, agentkit.OutboundEvent{Type: agentkit.EventMessageEnd, Data: toolStep}); err != nil {
		t.Fatal(err)
	}

	final, _ := json.Marshal(agentkit.MessageEndPayload{
		Message: agentkit.ModelMessage{
			Role:    "assistant",
			Content: []agentkit.ContentPart{{Type: "text", Text: "final answer"}},
		},
	})
	if err := emit(ctx, agentkit.OutboundEvent{Type: agentkit.EventMessageEnd, Data: final}); err != nil {
		t.Fatal(err)
	}

	sendMsg, _ := json.Marshal(agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "progress"}},
	})
	if err := emit(ctx, agentkit.OutboundEvent{Type: agentkit.EventAssistantMessage, Data: sendMsg}); err != nil {
		t.Fatal(err)
	}

	askPayload, _ := json.Marshal(permission.RequestPayload{
		Request: permission.Request{
			Kind: permission.KindQuestion,
			Question: &permission.Question{
				Prompt:  "pick one",
				Options: []permission.Option{{Label: "a"}, {Label: "b"}},
			},
		},
	})
	if err := emit(ctx, agentkit.OutboundEvent{Type: agentkit.EventPermissionRequest, Data: askPayload}); err != nil {
		t.Fatal(err)
	}

	end := telemetry.TurnEndFromAccum(ctx)
	if strings.Contains(end.Output, "internal") {
		t.Fatalf("tool step should not appear in user output: %q", end.Output)
	}
	for _, want := range []string{"final answer", "[send] progress", "[ask_user] pick one", "options: a, b"} {
		if !strings.Contains(end.Output, want) {
			t.Fatalf("output missing %q: %q", want, end.Output)
		}
	}
	if strings.Count(end.Output, "-------------") != 2 {
		t.Fatalf("expected 2 separators, got %q", end.Output)
	}
}
