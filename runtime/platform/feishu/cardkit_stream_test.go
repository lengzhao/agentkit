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

func TestBuildUnifiedStreamingCardJSON(t *testing.T) {
	raw := buildUnifiedStreamingCardJSON(true)
	var card map[string]any
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("unmarshal card: %v", err)
	}
	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	if len(elements) != 2 {
		t.Fatalf("elements len = %d, want 2", len(elements))
	}
	progressPanel := elements[0].(map[string]any)
	if progressPanel["tag"] != "collapsible_panel" || progressPanel["expanded"] != false {
		t.Fatalf("progress panel = %#v", progressPanel)
	}
	bodyPanel := elements[1].(map[string]any)
	if bodyPanel["tag"] != "collapsible_panel" || bodyPanel["expanded"] != true {
		t.Fatalf("body panel = %#v", bodyPanel)
	}
}

func TestBuildUnifiedStreamingCardJSONWithoutProgress(t *testing.T) {
	raw := buildUnifiedStreamingCardJSON(false)
	var card map[string]any
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("unmarshal card: %v", err)
	}
	body := card["body"].(map[string]any)
	elements := body["elements"].([]any)
	if len(elements) != 1 {
		t.Fatalf("elements len = %d, want 1", len(elements))
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
