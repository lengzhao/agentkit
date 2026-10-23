package slack

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/interaction"
)

func TestSubmittedSlackCardPreservesBlocksAndOptions(t *testing.T) {
	e := interaction.Event{RawCard: json.RawMessage(`{"blocks":[{"type":"header","text":{"type":"plain_text","text":"Risk"}},{"type":"section","block_id":"q","text":{"type":"mrkdwn","text":"Choose"},"accessory":{"type":"static_select","action_id":"risk","options":[{"text":{"type":"plain_text","text":"Low"},"value":"low"},{"text":{"type":"plain_text","text":"High"},"value":"high"}]}},{"type":"divider"},{"type":"input","label":{"type":"plain_text","text":"Reason"},"element":{"type":"plain_text_input","action_id":"why"}}]}`), Answers: []interaction.Answer{{Control: "risk", Value: "high", Label: "High"}, {Control: "why", Value: "Growth"}}}
	blocks, err := submittedSlackBlocks(e, "已提交\nHigh (high)")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(blocks)
	s := string(raw)
	for _, want := range []string{`"type":"header"`, `"block_id":"q"`, "Choose", "Low", "High", "Growth", `"type":"divider"`} {
		if !strings.Contains(s, want) {
			t.Errorf("lost %s: %s", want, s)
		}
	}
	for _, bad := range []string{`"accessory"`, `"type":"input"`, `"type":"static_select"`, `"type":"plain_text_input"`} {
		if strings.Contains(s, bad) {
			t.Errorf("interactive control retained: %s", s)
		}
	}
}

func TestSubmittedSlackCardRespectsSectionAndBlockLimits(t *testing.T) {
	var options []any
	for i := 0; i < 100; i++ {
		options = append(options, map[string]any{"text": map[string]any{"type": "plain_text", "text": strings.Repeat("选", 75)}, "value": i})
	}
	root := map[string]any{"blocks": []any{map[string]any{"type": "actions", "elements": []any{map[string]any{"type": "static_select", "action_id": "answer", "options": options}}}}}
	raw, _ := json.Marshal(root)
	blocks, err := submittedSlackBlocks(interaction.Event{RawCard: raw}, strings.Repeat("答", 4000))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) < 4 {
		t.Fatal("long text not split into safe sections")
	}
	for _, block := range blocks {
		raw, _ := json.Marshal(block)
		var section struct {
			Text struct {
				Text string `json:"text"`
			} `json:"text"`
		}
		_ = json.Unmarshal(raw, &section)
		if len([]rune(section.Text.Text)) > 3000 {
			t.Fatal("section exceeds Slack 3000-character limit")
		}
	}
	var full []any
	for i := 0; i < 50; i++ {
		full = append(full, map[string]any{"type": "divider"})
	}
	raw, _ = json.Marshal(map[string]any{"blocks": full})
	if _, err := submittedSlackBlocks(interaction.Event{RawCard: raw}, "已提交"); err == nil {
		t.Fatal("message block budget exceeded silently")
	}
}

func TestSubmittedSlackContextActionsBecomeReadOnly(t *testing.T) {
	e := interaction.Event{RawCard: json.RawMessage(`{"blocks":[{"type":"context_actions","elements":[{"type":"icon_button","action_id":"like","icon":"thumbsup","accessibility_label":"Like","value":"yes"}]}]}`)}
	blocks, err := submittedSlackBlocks(e, "已提交")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(blocks)
	if strings.Contains(string(raw), "context_actions") || strings.Contains(string(raw), "icon_button") || !strings.Contains(string(raw), "Like") {
		t.Fatalf("interactive context action retained: %s", raw)
	}
}
