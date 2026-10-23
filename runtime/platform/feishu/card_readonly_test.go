package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubmittedFeishuCardPreservesNativeLayout(t *testing.T) {
	raw := json.RawMessage(`{"schema":"2.0","header":{"title":{"tag":"plain_text","content":"理财风险偏好"},"template":"blue"},"config":{"wide_screen_mode":true},"body":{"elements":[{"tag":"column_set","columns":[{"tag":"column","elements":[{"tag":"button","name":"risk","text":{"tag":"plain_text","content":"进取型"},"value":{"action":"risk_level_test","value":"high"}}]}]},{"tag":"markdown","content":"请选择一个选项"}]}}`)
	before := string(raw)
	out, err := submittedFeishuCard(raw, "已提交\n已选择: 进取型 (high)")
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
		if _, err := submittedFeishuCard(raw, "已提交"); err == nil {
			t.Fatal("replaced unavailable original")
		}
	}
}

func TestSubmittedFeishuCardDisablesExtraControls(t *testing.T) {
	for _, tag := range []string{"button", "select_static"} {
		raw := json.RawMessage(`{"elements":[{"tag":"div","text":{"tag":"plain_text","content":"选择运动"},"extra":{"tag":"` + tag + `","value":{"action":"sport"}}}]}`)
		out, err := submittedFeishuCard(raw, "已提交")
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
		if _, err := submittedFeishuCard(raw, "已提交"); err == nil {
			t.Fatalf("unsupported %s reported closed", tag)
		}
	}
}
