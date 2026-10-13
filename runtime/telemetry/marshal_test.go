package telemetry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	captelemetry "github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/telemetry"
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

// thinking 是推理过程，不能混进 content 正文：导出为独立 thinking 字段，
// content 只保留用户可见回答。
func TestFormatMessageSeparatesThinking(t *testing.T) {
	t.Parallel()

	raw := telemetry.FormatMessage(agentkit.ModelMessage{
		Role: "assistant",
		Content: []agentkit.ContentPart{
			{Type: "thinking", Text: "let me reason"},
			{Type: "text", Text: "final answer"},
		},
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got["content"] != "final answer" {
		t.Fatalf("content = %#v, want only visible text", got["content"])
	}
	if got["thinking"] != "let me reason" {
		t.Fatalf("thinking = %#v, want dedicated field", got["thinking"])
	}
}

// generation input 走同一 messageEntryForExport，同样要保持 thinking 分离。
func TestFormatGenerationInputSeparatesThinking(t *testing.T) {
	t.Parallel()

	messages := []agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}},
		{Role: "assistant", Content: []agentkit.ContentPart{
			{Type: "thinking", Text: "hmm"},
			{Type: "text", Text: "hello"},
		}},
	}
	raw := telemetry.FormatGenerationInputForExport(nil, messages, 0, false)
	if !strings.Contains(raw, `"thinking":"hmm"`) {
		t.Fatalf("generation input missing dedicated thinking field: %s", raw)
	}
	if !strings.Contains(raw, `"content":"hello"`) {
		t.Fatalf("generation input content should exclude thinking: %s", raw)
	}
	if strings.Contains(raw, `"content":"hmmhello"`) || strings.Contains(raw, `"content":"hmm"`) {
		t.Fatalf("thinking leaked into content: %s", raw)
	}
}

func TestFormatMessageEncodesAttachments(t *testing.T) {
	t.Parallel()

	raw := telemetry.FormatMessage(agentkit.ModelMessage{
		Role: "user",
		Content: []agentkit.ContentPart{
			{Type: "text", Text: "describe this"},
			{Type: "image_url", MIME: "image/png", Source: "upload/shot.png", URL: "data:image/png;base64,QUJD"},
			{Type: "attachment_ref", MIME: "application/pdf", Source: "upload/doc.pdf"},
		},
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got["content"] != "describe this" {
		t.Fatalf("content = %#v", got["content"])
	}
	attachments, ok := got["attachments"].([]any)
	if !ok || len(attachments) != 2 {
		t.Fatalf("attachments = %#v", got["attachments"])
	}
	first, ok := attachments[0].(map[string]any)
	if !ok || first["type"] != "image_url" || first["source"] != "upload/shot.png" {
		t.Fatalf("image attachment = %#v", first)
	}
	if url, _ := first["url"].(string); !strings.Contains(url, "bytes") {
		t.Fatalf("url summary = %q", url)
	}
	second, ok := attachments[1].(map[string]any)
	if !ok || second["type"] != "attachment_ref" || second["source"] != "upload/doc.pdf" {
		t.Fatalf("file attachment = %#v", second)
	}
}

func TestSummarizeMessagesIncludesAttachments(t *testing.T) {
	t.Parallel()

	raw := telemetry.SummarizeMessages([]agentkit.ModelMessage{{
		Role: "user",
		Content: []agentkit.ContentPart{
			{Type: "text", Text: "hello"},
			{Type: "image_url", MIME: "image/jpeg", Source: "upload/a.jpg", URL: "data:image/jpeg;base64,abc"},
		},
	}}, 8192, false)
	if !strings.Contains(raw, `"attachments"`) || !strings.Contains(raw, "upload/a.jpg") {
		t.Fatalf("SummarizeMessages = %q", raw)
	}
	if strings.Contains(raw, "data:image/jpeg;base64,abc") {
		t.Fatalf("SummarizeMessages should not include raw data URL: %q", raw)
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
	telemetry.RecordTurnUsage(ctx, captelemetry.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
	telemetry.RecordTurnUsage(ctx, captelemetry.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5})

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
	if !strings.Contains(end.Output, "internal") {
		t.Fatalf("tool step text should appear in turn output: %q", end.Output)
	}
	for _, want := range []string{"internal", "final answer", "[send] progress", "[ask_user] pick one", "options: a, b"} {
		if !strings.Contains(end.Output, want) {
			t.Fatalf("output missing %q: %q", want, end.Output)
		}
	}
	if strings.Count(end.Output, "-------------") != 3 {
		t.Fatalf("expected 3 separators, got %q", end.Output)
	}
}
