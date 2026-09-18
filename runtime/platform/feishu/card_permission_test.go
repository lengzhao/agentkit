package feishu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func TestRenderCardMapStaticDisablesInteraction(t *testing.T) {
	card := common.ConfirmedPermissionCard("Pick one", "A")
	m := renderCardMap(card, "session-1")
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `"enable_forward_interaction":false`) || !strings.Contains(body, `"update_multi":false`) {
		t.Fatalf("static card config missing: %s", body)
	}
	if strings.Contains(body, `"tag":"button"`) || strings.Contains(body, `"tag":"action"`) {
		t.Fatalf("static card must not contain buttons: %s", body)
	}
}

func TestRenderCardMapStacksLongListItem(t *testing.T) {
	card := common.NewCard().Title("问题", "blue").Markdown("选一个").Build()
	card.Elements = append(card.Elements, common.CardListItem{
		Text:     "这是一段明显超过一行宽度的选项说明，右侧再塞按钮会挤在一起",
		BtnText:  "这是一段明显超过一行宽度的选项说明，右侧再塞按钮会挤在一起",
		BtnType:  "default",
		BtnValue: "perm:0",
	})
	m := renderCardMap(card, "session-1")
	elements, ok := m["elements"].([]map[string]any)
	if !ok || len(elements) < 2 {
		t.Fatalf("elements = %#v", m["elements"])
	}
	item := elements[1]
	cols, ok := item["columns"].([]map[string]any)
	if !ok || len(cols) != 1 {
		t.Fatalf("long option should be a single stacked column: %#v", item)
	}
	inner, ok := cols[0]["elements"].([]map[string]any)
	if !ok || len(inner) != 2 {
		t.Fatalf("stacked column elements = %#v", cols[0]["elements"])
	}
	if inner[0]["tag"] != "markdown" || inner[1]["tag"] != "button" {
		t.Fatalf("expected markdown then button, got %#v", inner)
	}
	if inner[1]["width"] != "fill" {
		t.Fatalf("button width = %#v", inner[1]["width"])
	}
}

func TestRenderCardMapKeepsShortListItemInline(t *testing.T) {
	card := common.NewCard().Title("需要确认", "orange").Build()
	card.Elements = append(card.Elements, common.CardListItem{
		Text: "允许", BtnText: "允许", BtnType: "primary", BtnValue: "perm:allow",
	})
	m := renderCardMap(card, "")
	elements, ok := m["elements"].([]map[string]any)
	if !ok || len(elements) != 1 {
		t.Fatalf("elements = %#v", m["elements"])
	}
	cols, ok := elements[0]["columns"].([]map[string]any)
	if !ok || len(cols) != 2 {
		t.Fatalf("short option should stay side-by-side: %#v", elements[0])
	}
}
