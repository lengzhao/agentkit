package feishu

import (
	"strings"
	"testing"
)

func TestBuildFinalPreviewCardJSONPlainText(t *testing.T) {
	text := "Hi！有什么我可以帮你的？"
	cardJSON := buildFinalPreviewCardJSON(text)

	if strings.Contains(cardJSON, `{"text":`) {
		t.Fatalf("card should contain plain text, not IM API JSON wrapper: %s", cardJSON)
	}
	if !strings.Contains(cardJSON, text) {
		t.Fatalf("card should contain original text %q, got %s", text, cardJSON)
	}
}

func TestBuildFinalPreviewCardJSONMarkdown(t *testing.T) {
	text := "**hello** world"
	cardJSON := buildFinalPreviewCardJSON(text)

	if strings.Contains(cardJSON, `{"text":`) {
		t.Fatalf("card should not contain IM API JSON wrapper: %s", cardJSON)
	}
	if !strings.Contains(cardJSON, "hello") {
		t.Fatalf("card should contain markdown content, got %s", cardJSON)
	}
}
