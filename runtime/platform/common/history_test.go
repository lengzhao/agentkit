package common

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

func TestMessagesFromAskUserEvents(t *testing.T) {
	callInput, _ := json.Marshal(askUserCallInput{
		Question: "请选择一个选项。",
		Options:  []string{"选项1", "选项2", "选项3"},
	})
	callData, _ := json.Marshal(agentkit.ToolCall{
		Name:  "ask_user",
		Input: callInput,
	})
	resultData, _ := json.Marshal(agentkit.ToolResult{
		Name:    "ask_user",
		Content: `{"answered":true,"answer":"选项1","selected":0}`,
	})
	userData, _ := json.Marshal(agentkit.ModelMessage{
		Role:    "user",
		Content: []agentkit.ContentPart{{Type: "text", Text: "ask user: 3个选项"}},
	})
	assistantData, _ := json.Marshal(agentkit.ModelMessage{
		Role:    "assistant",
		Content: []agentkit.ContentPart{{Type: "text", Text: "你选择的是：选项1。"}},
	})

	events := []agentkit.SessionEvent{
		{Type: agentkit.EventUserMessage, Data: userData, UserID: "demo", CreatedAt: time.Unix(1, 0)},
		{Type: agentkit.EventToolCall, Data: callData, CreatedAt: time.Unix(2, 0)},
		{Type: agentkit.EventToolResult, Data: resultData, UserID: "demo", CreatedAt: time.Unix(3, 0)},
		{Type: agentkit.EventAssistantMessage, Data: assistantData, CreatedAt: time.Unix(4, 0)},
	}

	out := MessagesFromEvents(events)
	if len(out) != 4 {
		t.Fatalf("len(out) = %d, want 4", len(out))
	}
	if out[0].Role != "user" || out[0].Content != "ask user: 3个选项" {
		t.Fatalf("user message = %+v", out[0])
	}
	if out[1].Role != "assistant" || !strings.Contains(out[1].Content, "请选择一个选项。") {
		t.Fatalf("ask prompt = %+v", out[1])
	}
	if !strings.Contains(out[1].Content, "1. 选项1") {
		t.Fatalf("ask options = %+v", out[1])
	}
	if out[2].Role != "user" || out[2].Content != "选项1" {
		t.Fatalf("user answer = %+v", out[2])
	}
	if out[3].Role != "assistant" || out[3].Content != "你选择的是：选项1。" {
		t.Fatalf("assistant answer = %+v", out[3])
	}
}

func TestMessageFromEventSkipsToolOnlyAssistant(t *testing.T) {
	data, _ := json.Marshal(agentkit.ModelMessage{
		Role: "assistant",
		ToolCalls: []agentkit.ToolCall{{
			Name:  "ask_user",
			Input: json.RawMessage(`{"question":"q","options":["a"]}`),
		}},
	})
	ev := agentkit.SessionEvent{Type: agentkit.EventAssistantMessage, Data: data}
	if m := MessageFromEvent(ev); m != nil {
		t.Fatalf("expected nil for tool-only assistant, got %+v", m)
	}
}

func TestFormatPermissionRequest(t *testing.T) {
	text := FormatPermissionRequest(permission.RequestPayload{
		Request: permission.Request{
			Kind: permission.KindQuestion,
			Question: &permission.Question{
				Prompt:  "选一个",
				Options: []permission.Option{{Label: "A"}, {Label: "B"}},
			},
		},
	})
	if !strings.Contains(text, "选一个") || !strings.Contains(text, "1. A") {
		t.Fatalf("question history = %q", text)
	}
}
