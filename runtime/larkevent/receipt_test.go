package larkevent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	cap "github.com/lengzhao/agentkit/cap/larkevent"
)

func TestReceiptSummaryProjectsOnlyPublicResourceFields(t *testing.T) {
	title, body := ReceiptSummary(cap.Event{Type: "minutes.minute.generated_v1", EventID: "e1", Raw: json.RawMessage(`{"event":{"minute_source":{"source_entity_id":7703573987080277776},"minute_token":"minute-1","access_token":"secret","subscriber_ids":[{"open_id":"private-user"}]}}`)}, time.Time{})
	if !strings.Contains(title, "minutes.minute.generated_v1") || !strings.Contains(body, "7703573987080277776") || !strings.Contains(body, "minute-1") || strings.Contains(body, "secret") || strings.Contains(body, "private-user") || strings.Contains(body, "0001-") {
		t.Fatalf("%s %s", title, body)
	}
}
func TestReceiptSummarySanitizesAndBoundsScalarDisplay(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"event": map[string]string{"title": "`line\nbreak`" + strings.Repeat("中", 180)}})
	_, body := ReceiptSummary(cap.Event{Type: "custom", Raw: raw}, time.Time{})
	if strings.Contains(body, "line\nbreak") || strings.Contains(body, "`line") || strings.Count(body, "中") > 160 {
		t.Fatalf("%s", body)
	}
}
