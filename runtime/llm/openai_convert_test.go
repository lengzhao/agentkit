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
