package feishu

import (
	"encoding/json"
	"strings"
	"testing"
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
	}, "", true, 0)
	if !strings.Contains(card, `"expanded":false`) {
		t.Fatalf("expected collapsed progress panel, got %q", card)
	}
	if !strings.Contains(card, `"streaming_mode":true`) {
		t.Fatalf("expected streaming mode during progress updates, got %q", card)
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
