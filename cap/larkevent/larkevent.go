// Package larkevent 定义 Lark/Feishu 自定义事件的处理契约。
//
// platform/feishu、platform/lark 除内建 IM 事件（消息、卡片回调等）外，
// 可通过 deps.eventHandlers 注入实现本接口的插件，按事件类型订阅
// Lark 开放平台事件（drive、calendar、contact、审批、会议等）。
// 事件类型需与 Lark 开发者后台的事件订阅保持一致。
package larkevent

import (
	"context"
	"encoding/json"
)

// Event 是平台投递给 Handler 的通用事件载荷。
type Event struct {
	// Type 是事件类型字符串，如 drive.file.created_v1、calendar.calendar.changed_v4。
	Type string `json:"type"`
	// EventID 是 header.event_id，可用于去重。
	EventID string `json:"eventId,omitempty"`
	// ChatID 是从事件体解析出的群 chat_id；事件不带群上下文时为空。
	ChatID string `json:"chatId,omitempty"`
	// UserID 是从事件体解析出的操作者 open_id；无法解析时为空。
	UserID string `json:"userId,omitempty"`
	// Raw 是解密后的完整事件 JSON（schema 2.0，含 header 与 event）。
	Raw json.RawMessage `json:"raw"`
}

// Handler 消费一类或多类 Lark 事件。实现由插件提供，
// 经 platform/feishu、platform/lark 的 deps.eventHandlers 注入。
//
// 同一事件类型可被多个 Handler 订阅，平台按注入顺序逐个投递；
// 单个 Handler 返回错误只记录日志，不影响其他 Handler。
type Handler interface {
	// EventTypes 声明订阅的事件类型列表。
	EventTypes() []string
	// Handle 处理单个事件。
	Handle(ctx context.Context, event Event) error
}
