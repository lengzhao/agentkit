package feishu

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildStreamingBodyCardEntityJSON(t *testing.T) {
	raw := buildStreamingBodyCardEntityJSON()
	var card map[string]any
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("unmarshal card: %v", err)
	}
	config, ok := card["config"].(map[string]any)
	if !ok {
		t.Fatal("missing config")
	}
	if config["streaming_mode"] != true {
		t.Fatalf("streaming_mode = %v", config["streaming_mode"])
	}
	body, ok := card["body"].(map[string]any)
	if !ok {
		t.Fatal("missing body")
	}
	elements, ok := body["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatal("missing body elements")
	}
	el, ok := elements[0].(map[string]any)
	if !ok {
		t.Fatal("invalid element")
	}
	if el["element_id"] != bodyStreamElementID {
		t.Fatalf("element_id = %v", el["element_id"])
	}
}

func TestBuildRichCardProgressPanelCollapsed(t *testing.T) {
	card := buildRichCard(cardStatusWorking, "", []toolStep{
		{Kind: toolStepKindTool, Name: "Read", Summary: "README.md"},
	}, "", true, 5*time.Second)
	if !strings.Contains(card, `"expanded":false`) {
		t.Fatalf("expected collapsed progress panel, got %q", card)
	}
	if !strings.Contains(card, `"streaming_mode":true`) {
		t.Fatalf("expected streaming mode during progress updates, got %q", card)
	}
	if !strings.Contains(card, "处理中 · 1 个工具") || !strings.Contains(card, "⏱") {
		t.Fatalf("expected panel title with tools and elapsed time, got %q", card)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(card), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := parsed["header"]; ok {
		t.Fatalf("in-progress rich card should omit top header, got %q", card)
	}
}

func TestBuildRichCardDoneShowsStatusLine(t *testing.T) {
	card := buildRichCard(cardStatusDone, "", nil, "hello", false, 3*time.Second)
	if !strings.Contains(card, "☑️ 用时") || !strings.Contains(card, "hello") {
		t.Fatalf("expected done status line and body, got %q", card)
	}
	if strings.Contains(card, `"header"`) {
		t.Fatalf("simplified card should not use top header, got %q", card)
	}
}

func TestRichCardBodyMarkdownDone(t *testing.T) {
	got := richCardBodyMarkdown(cardStatusDone, "reply", 2*time.Second, false)
	if !strings.HasPrefix(got, "☑️ 用时") || !strings.Contains(got, "reply") {
		t.Fatalf("unexpected body markdown: %q", got)
	}
}

func TestBuildIMCardEntityContent(t *testing.T) {
	content := buildIMCardEntityContent("7355372766134157313")
	if !strings.Contains(content, `"type":"card"`) {
		t.Fatalf("unexpected content: %s", content)
	}
	if !strings.Contains(content, `"card_id":"7355372766134157313"`) {
		t.Fatalf("unexpected content: %s", content)
	}
}
