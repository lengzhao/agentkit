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
