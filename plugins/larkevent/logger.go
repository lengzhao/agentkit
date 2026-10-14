// Package logger 是 cap/larkevent.Handler 的示例插件：
// 订阅一类或多类 Lark 自定义事件，收到后以 slog 记录。
// 注入 platform/feishu、platform/lark 的 deps.eventHandlers 使用。
package logger

import (
	"context"
	"log/slog"
	"strings"

	caplarkevent "github.com/lengzhao/agentkit/cap/larkevent"
	"github.com/lengzhao/pluginkit"
)

// Config 声明本实例订阅的事件类型列表。
type Config struct {
	// EventTypes 是 Lark 事件类型字符串（如 drive.file.created_v1），
	// 需与 Lark 开发者后台的事件订阅保持一致。
	EventTypes []string `json:"eventTypes"`
	// LogRaw 为 true 时记录完整事件 JSON（默认只记录类型与上下文摘要）。
	LogRaw bool `json:"logRaw,omitempty"`
}

type Deps struct{}

// Handler 实现 cap/larkevent.Handler。
type Handler struct {
	eventTypes []string
	logRaw     bool
}

func (h *Handler) EventTypes() []string { return h.eventTypes }

func (h *Handler) Handle(_ context.Context, event caplarkevent.Event) error {
	attrs := []any{
		"event_type", event.Type,
		"event_id", event.EventID,
		"chat_id", event.ChatID,
		"user_id", event.UserID,
	}
	if h.logRaw {
		attrs = append(attrs, "raw", string(event.Raw))
	}
	slog.Info("lark-event/logger: event received", attrs...)
	return nil
}

// New 注册 lark-event/logger：按 config.eventTypes 订阅 Lark 自定义事件并打日志。
func New(cfg Config, _ Deps) (*Handler, error) {
	eventTypes := make([]string, 0, len(cfg.EventTypes))
	for _, t := range cfg.EventTypes {
		if t = strings.TrimSpace(t); t != "" {
			eventTypes = append(eventTypes, t)
		}
	}
	return &Handler{eventTypes: eventTypes, logRaw: cfg.LogRaw}, nil
}

func init() {
	pluginkit.Register("lark-event/logger", New)
}
