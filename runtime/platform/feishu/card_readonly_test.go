package feishu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/cap/interaction"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func TestSubmittedFeishuCardPreservesNativeLayout(t *testing.T) {
	raw := json.RawMessage(`{"schema":"2.0","header":{"title":{"tag":"plain_text","content":"理财风险偏好"},"template":"blue"},"config":{"wide_screen_mode":true},"body":{"elements":[{"tag":"column_set","columns":[{"tag":"column","elements":[{"tag":"button","name":"risk","text":{"tag":"plain_text","content":"进取型"},"value":{"action":"risk_level_test","value":"high"}}]}]},{"tag":"markdown","content":"请选择一个选项"}]}}`)
	before := string(raw)
	out, err := submittedFeishuCard(interaction.Event{RawCard: raw}, "已提交\n已选择: 进取型 (high)")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"schema":"2.0"`, `"template":"blue"`, `"tag":"column_set"`, `"tag":"button"`, `"disabled":true`, "请选择一个选项", "进取型 (high)"} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %s: %s", want, out)
		}
	}
	if string(raw) != before {
		t.Fatal("mutated event snapshot")
	}
	if strings.Count(out, "已提交") != 1 {
		t.Fatal("duplicated submission status")
	}
}
func TestSubmittedFeishuCardRefusesMissingOriginal(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{"title":"summary only"}`), json.RawMessage(`{"title":"summary","elements":[[{"tag":"text","text":"simplified content"}]]}`)} {
		if _, err := submittedFeishuCard(interaction.Event{RawCard: raw}, "已提交"); err == nil {
			t.Fatal("replaced unavailable original")
		}
	}
}

func TestSubmittedFeishuCardDisablesExtraControls(t *testing.T) {
	for _, tag := range []string{"button", "select_static"} {
		raw := json.RawMessage(`{"elements":[{"tag":"div","text":{"tag":"plain_text","content":"选择运动"},"extra":{"tag":"` + tag + `","value":{"action":"sport"}}}]}`)
		out, err := submittedFeishuCard(interaction.Event{RawCard: raw}, "已提交")
		if err != nil {
			t.Fatal(err)
		}
		var card struct {
			Elements []struct {
				Extra struct {
					Disabled bool `json:"disabled"`
				} `json:"extra"`
			} `json:"elements"`
		}
		if err := json.Unmarshal([]byte(out), &card); err != nil {
			t.Fatal(err)
		}
		if !card.Elements[0].Extra.Disabled {
			t.Fatalf("extra %s remained interactive: %s", tag, out)
		}
	}
}

func TestSubmittedFeishuCardRejectsUnsupportedControls(t *testing.T) {
	for _, tag := range []string{"overflow", "future_control"} {
		raw := json.RawMessage(`{"elements":[{"tag":"div","extra":{"tag":"` + tag + `","value":{"action":"submit"}}}]}`)
		if _, err := submittedFeishuCard(interaction.Event{RawCard: raw}, "已提交"); err == nil {
			t.Fatalf("unsupported %s reported closed", tag)
		}
	}
}

func TestSubmittedFeishuCardMarksActualButtonSelection(t *testing.T) {
	raw := json.RawMessage(`{"schema":"2.0","body":{"elements":[{"tag":"button","type":"primary","text":{"tag":"plain_text","content":"广东"},"behaviors":[{"type":"callback","value":{"choice":"广东","test_id":"food"}}]},{"tag":"button","type":"default","text":{"tag":"plain_text","content":"北京"},"behaviors":[{"type":"callback","value":{"choice":"北京","test_id":"food"}}]},{"tag":"button","text":{"tag":"plain_text","content":"重庆"},"behaviors":[{"type":"callback","value":{"choice":"重庆","test_id":"food"}}]}]}}`)
	e := interaction.Event{RawCard: raw, RawAction: json.RawMessage(`{"tag":"button","value":{"choice":"北京","test_id":"food"}}`), Answers: []interaction.Answer{{Value: map[string]any{"choice": "北京", "test_id": "food"}}}}
	common.EnrichInteractionCard(&e)
	out, err := submittedFeishuCard(e, common.InteractionSubmissionSummary(e))
	if err != nil {
		t.Fatal(err)
	}
	var card struct {
		Body struct {
			Elements []struct {
				Tag      string `json:"tag"`
				Type     string `json:"type"`
				Disabled bool   `json:"disabled"`
				Text     struct {
					Content string `json:"content"`
				} `json:"text"`
				Content string `json:"content"`
			} `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &card); err != nil {
		t.Fatal(err)
	}
	buttons := card.Body.Elements
	if buttons[0].Type != "default" || buttons[0].Text.Content != "广东" {
		t.Fatalf("old default still appears selected: %#v", buttons[0])
	}
	if buttons[1].Type != "primary" || buttons[1].Text.Content != "✓ 北京" {
		t.Fatalf("actual selection not marked: %#v", buttons[1])
	}
	for _, button := range buttons[:3] {
		if !button.Disabled {
			t.Fatal("button remained interactive")
		}
	}
	if buttons[3].Content != "已提交\n北京" {
		t.Fatalf("bad summary: %s", buttons[3].Content)
	}
	if string(e.RawCard) != string(raw) {
		t.Fatal("original card changed")
	}
}

func TestMarkSubmittedFeishuButtonMatchesOnlyUniquePayload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		second string
		marked bool
	}{
		{"legacy value", `9007199254740993`, true},
		{"ambiguous", `9007199254740993`, false},
		{"unmatched", `9007199254740994`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := "9007199254740992"
			if tc.name == "ambiguous" {
				first = tc.second
			}
			buttons := []map[string]any{
				{"type": "primary", "value": json.Number(first), "text": map[string]any{"content": "A"}},
				{"value": json.Number(tc.second), "text": map[string]any{"content": "B"}},
			}
			markSubmittedFeishuButton(buttons, interaction.Event{RawAction: json.RawMessage(`{"tag":"button","value":9007199254740993}`)})
			if got := buttons[1]["text"].(map[string]any)["content"]; (got == "✓ B") != tc.marked {
				t.Fatalf("unexpected selection: %v", buttons)
			}
		})
	}
}
