package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

// rawCardContent wraps a schema 2.0 card the way GET message returns it with
// card_msg_content_type=raw_card_content.
func rawCardContent(t *testing.T, card string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"json_card": card})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestExtractInteractiveCardText_RawSchema2(t *testing.T) {
	card := `{
	  "header": {"property": {
	    "title": {"tag": "plain_text", "property": {"content": "New plugin"}},
	    "subtitle": {"tag": "plain_text", "property": {"content": "Short subtitle"}},
	    "textTagList": [{"tag": "text_tag", "property": {"text": {"tag": "plain_text", "property": {"content": "Beta"}}}}]
	  }},
	  "body": {"tag": "body", "property": {"elements": [
	    {"tag": "markdown", "property": {"elements": [
	      {"tag": "plain_text", "property": {"content": "Hello "}},
	      {"tag": "plain_text", "property": {"content": "world", "textStyle": {"attributes": ["bold"]}}},
	      {"tag": "plain_text", "property": {"content": ", see "}},
	      {"tag": "link", "property": {"content": "docs", "url": {"url": "https://example.com/docs"}}},
	      {"tag": "br", "property": {}},
	      {"tag": "at", "property": {"userID": "ou_123"}},
	      {"tag": "list", "property": {"items": [
	        {"type": "ul", "level": 0, "elements": [
	          {"tag": "plain_text", "property": {"content": "First", "textStyle": {"attributes": ["bold"]}}},
	          {"tag": "plain_text", "property": {"content": ": item"}}
	        ]},
	        {"type": "ul", "level": 1, "elements": [{"tag": "plain_text", "property": {"content": "Nested"}}]}
	      ]}}
	    ]}},
	    {"tag": "column_set", "property": {"columns": [
	      {"tag": "column", "property": {"elements": [{"tag": "markdown", "property": {"elements": [{"tag": "plain_text", "property": {"content": "Left col"}}]}}]}},
	      {"tag": "column", "property": {"elements": [{"tag": "markdown", "property": {"elements": [{"tag": "plain_text", "property": {"content": "Right col"}}]}}]}}
	    ]}},
	    {"tag": "collapsible_panel", "property": {
	      "header": {"title": {"tag": "markdown", "property": {"elements": [{"tag": "plain_text", "property": {"content": "Panel title"}}]}}},
	      "elements": [{"tag": "markdown", "property": {"elements": [{"tag": "plain_text", "property": {"content": "Panel body"}}]}}]
	    }},
	    {"tag": "img", "property": {"alt": {"tag": "plain_text", "property": {"content": "Screenshot"}}, "imageID": "1"}},
	    {"tag": "hr", "property": {}},
	    {"tag": "button", "property": {
	      "text": {"tag": "plain_text", "property": {"content": "Open repo"}},
	      "actions": [{"type": "open_url", "action": {"url": "https://example.com/repo"}}]
	    }},
	    {"tag": "button", "property": {"text": {"tag": "plain_text", "property": {"content": "Approve"}}}}
	  ]}}
	}`
	got := extractInteractiveCardText(rawCardContent(t, card))
	want := strings.Join([]string{
		"## New plugin [Beta]",
		"Short subtitle",
		"Hello **world**, see [docs](https://example.com/docs)\n@ou_123\n- **First**: item\n  - Nested",
		"Left col",
		"Right col",
		"Panel title",
		"Panel body",
		"[Image: Screenshot]",
		"---",
		"[Button: Open repo](https://example.com/repo)",
		"[Button: Approve]",
	}, "\n")
	if got != want {
		t.Fatalf("unexpected output.\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestExtractInteractiveCardText_Legacy(t *testing.T) {
	// Degraded event payload / legacy card: title + rows of inline elements.
	content := `{"title":"Legacy title","elements":[[{"tag":"text","text":"Line "},{"tag":"a","text":"link","href":"https://example.com"}],[{"tag":"text","text":"Second"}]]}`
	got := extractInteractiveCardText(content)
	want := "Legacy title\nLine [link](https://example.com)\nSecond"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractInteractiveCardText_Empty(t *testing.T) {
	for _, content := range []string{"not json", `{"title":null,"elements":[[{"tag":"text","text":""}]]}`} {
		if got := extractInteractiveCardText(content); got != "[interactive card]" {
			t.Fatalf("content %q: got %q", content, got)
		}
	}
}

func TestExtractInteractiveCardText_ListLevelBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		level  int
		indent int
	}{
		{"negative", -1, 0}, {"normal", 2, 2}, {"excessive", 1000000, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("unexpected panic: %v", r)
				}
			}()
			content, err := json.Marshal(map[string]any{"elements": []any{map[string]any{"tag": "list", "items": []any{map[string]any{"level": tc.level, "elements": []any{map[string]any{"tag": "text", "text": "item"}}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			got := extractInteractiveCardText(string(content))
			want := strings.Repeat("  ", tc.indent) + "- item"
			if got != want {
				t.Fatalf("got output length %d, want %q", len(got), want)
			}
		})
	}
}

func TestInteractiveCardText_Fallback(t *testing.T) {
	event := `{"title":"Event title","elements":[[{"tag":"text","text":"Event body"}]]}`
	for _, fetched := range []string{"", "[interactive card]", "  \n "} {
		if got := interactiveCardText(fetched, event); got != "Event title\nEvent body" {
			t.Errorf("fetched %q: got %q", fetched, got)
		}
	}
	if got := interactiveCardText("Fetched body", event); got != "Fetched body" {
		t.Errorf("got %q", got)
	}
	if got := interactiveCardText("[interactive card]", "invalid"); got != "[interactive card]" {
		t.Errorf("got %q", got)
	}
}
