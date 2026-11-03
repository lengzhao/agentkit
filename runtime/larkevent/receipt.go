// Package larkevent contains event presentation independent of the native SDK.
package larkevent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	caplarkevent "github.com/lengzhao/agentkit/cap/larkevent"
)

// ReceiptSummary projects only the event's whitelisted resource fields. Native
// subscription, routing and message creation remain the platform's responsibility.
func ReceiptSummary(evt caplarkevent.Event, createdAt time.Time) (string, string) {
	var body strings.Builder
	body.WriteString("事件已受理，处理完成后会在此汇报结果。")
	field := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		value = strings.NewReplacer("`", "ˋ", "\n", " ", "\r", " ").Replace(value)
		fmt.Fprintf(&body, "\n**%s**：`%s`", label, receiptField(value))
	}
	if !createdAt.IsZero() {
		field("发生时间", createdAt.Local().Format("2006-01-02 15:04:05 -07:00"))
	}
	field("事件 ID", evt.EventID)
	var envelope struct {
		Event map[string]any `json:"event"`
	}
	decoder := json.NewDecoder(bytes.NewReader(evt.Raw))
	decoder.UseNumber() // resource IDs can exceed float64's exact integer range
	if decoder.Decode(&envelope) == nil {
		for _, detail := range []struct{ path, label string }{
			{"title", "标题"}, {"name", "名称"}, {"meeting_id", "会议 ID"},
			{"minute_source.source_entity_id", "来源 ID"}, {"minute_source.source_type", "来源类型"},
			{"minute_token", "妙记 token"}, {"file_token", "文件 token"},
			{"document_id", "文档 ID"}, {"file_id", "文件 ID"},
		} {
			var value any = envelope.Event
			for _, segment := range strings.Split(detail.path, ".") {
				object, ok := value.(map[string]any)
				if !ok {
					value = nil
					break
				}
				value = object[segment]
			}
			switch scalar := value.(type) {
			case string:
				field(detail.label, scalar)
			case json.Number:
				field(detail.label, string(scalar))
			}
		}
	}
	return fmt.Sprintf("收到事件 %s，正在处理…", evt.Type), body.String()
}

func receiptField(value string) string {
	runes := []rune(value)
	if len(runes) > 160 {
		return string(runes[:160]) + "..."
	}
	return value
}
