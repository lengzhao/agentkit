package llm

import (
	"testing"

	"github.com/lengzhao/agentkit"
	openai "github.com/sashabaranov/go-openai"
)

func TestToChatCompletionMessagesMultimodal(t *testing.T) {
	t.Parallel()

	msgs := toChatCompletionMessages([]agentkit.ModelMessage{{
		Role: "user",
		Content: []agentkit.ContentPart{
			{Type: "text", Text: "describe "},
			{Type: "image", URL: "https://example.com/a.png", Detail: "low"},
		},
	}})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if len(msgs[0].MultiContent) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(msgs[0].MultiContent))
	}
	if msgs[0].MultiContent[0].Type != openai.ChatMessagePartTypeText {
		t.Fatalf("unexpected first part: %#v", msgs[0].MultiContent[0])
	}
	if msgs[0].MultiContent[1].ImageURL == nil || msgs[0].MultiContent[1].ImageURL.URL != "https://example.com/a.png" {
		t.Fatalf("unexpected image part: %#v", msgs[0].MultiContent[1])
	}
}

func TestParseAPIMode(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":              openAIAPIChat,
		"chat":          openAIAPIChat,
		"responses":     openAIAPIResponses,
		"RESPONSE":      openAIAPIResponses,
		"custom-vendor": "custom-vendor",
	}
	for in, want := range cases {
		if got := parseAPIMode(in); got != want {
			t.Fatalf("parseAPIMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToResponsesInputSimpleText(t *testing.T) {
	t.Parallel()

	instructions, input := toResponsesInput([]agentkit.ModelMessage{
		{Role: "system", Content: []agentkit.ContentPart{{Type: "text", Text: "be helpful"}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hi"}}},
	})
	if instructions != "be helpful" {
		t.Fatalf("instructions = %q", instructions)
	}
	if input != "hi" {
		t.Fatalf("input = %#v, want hi", input)
	}
}

func TestToResponsesInputMultiTurn(t *testing.T) {
	t.Parallel()

	_, input := toResponsesInput([]agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []agentkit.ContentPart{{Type: "text", Text: "hi there"}}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "weather?"}}},
	})
	items, ok := input.([]any)
	if !ok {
		t.Fatalf("input = %T, want []any", input)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	for i, raw := range items {
		msg, ok := raw.(openai.ResponseInputMessage)
		if !ok {
			t.Fatalf("item %d = %T", i, raw)
		}
		switch content := msg.Content.(type) {
		case string:
		case []any:
			for j, part := range content {
				switch part.(type) {
				case openai.ResponseInputText, openai.ResponseInputImage, openai.ResponseOutputContent:
				default:
					t.Fatalf("item %d part %d = %T", i, j, part)
				}
			}
		default:
			t.Fatalf("item %d content = %T, want string or []any", i, content)
		}
		if msg.Role == openai.ChatMessageRoleAssistant {
			switch content := msg.Content.(type) {
			case string:
			case []any:
				for _, part := range content {
					if out, ok := part.(openai.ResponseOutputContent); ok && out.Type != "output_text" {
						t.Fatalf("assistant part type = %q", out.Type)
					}
				}
			}
		}
	}
}

func TestToResponsesInputAssistantHistoryUsesOutputText(t *testing.T) {
	t.Parallel()

	_, input := toResponsesInput([]agentkit.ModelMessage{
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []agentkit.ContentPart{
			{Type: "thinking", Text: "internal"},
			{Type: "text", Text: "hi there"},
		}},
		{Role: "user", Content: []agentkit.ContentPart{{Type: "text", Text: "weather?"}}},
	})
	items, ok := input.([]any)
	if !ok {
		t.Fatalf("input = %T, want []any", input)
	}
	assistant, ok := items[1].(openai.ResponseInputMessage)
	if !ok || assistant.Role != openai.ChatMessageRoleAssistant {
		t.Fatalf("item 1 = %#v", items[1])
	}
	if assistant.Content != "hi there" {
		t.Fatalf("assistant content = %#v, want plain text", assistant.Content)
	}
}

func TestToResponsesRequestHostedTools(t *testing.T) {
	t.Parallel()

	req, err := toResponsesRequest("gpt-5.4", nil, []agentkit.ToolSpec{{
		Name:        "read",
		Description: "read file",
	}}, []HostedToolConfig{{
		Type: "web_search",
		Parameters: map[string]any{
			"search_context_size": "medium",
		},
	}}, nil)
	if err != nil {
		t.Fatalf("toResponsesRequest: %v", err)
	}
	if len(req.Tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(req.Tools))
	}
	if req.Tools[1].Type != openai.ToolTypeWebSearch {
		t.Fatalf("hosted tool type = %q", req.Tools[1].Type)
	}
	if req.Tools[1].Parameters["search_context_size"] != "medium" {
		t.Fatalf("hosted tool params = %#v", req.Tools[1].Parameters)
	}
	if len(req.Include) != 1 || req.Include[0] != openai.ResponseIncludeWebSearchCallActionSources {
		t.Fatalf("include = %#v", req.Include)
	}
}

func TestToHostedResponseToolsRequiresType(t *testing.T) {
	t.Parallel()

	_, err := toHostedResponseTools([]HostedToolConfig{{Type: "  "}})
	if err == nil {
		t.Fatal("expected error for empty hosted tool type")
	}
}
