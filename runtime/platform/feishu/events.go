package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/lengzhao/agentkit"
	caplarkevent "github.com/lengzhao/agentkit/cap/larkevent"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// customEventRoute 聚合一个事件类型的所有投递目标：
// deps.eventHandlers 注入的 handler 插件，以及平台内置的 agent 直转。
type customEventRoute struct {
	handlers []caplarkevent.Handler
	toAgent  bool
}

// registerCustomEvents 按 deps.eventHandlers 声明的订阅与
// config.forwardEventsToAgent 列表，向 dispatcher 注册 OnCustomizedEvent。
// 事件类型同时出现在两处时同一事件既投递插件也直转 agent。
func (p *Platform) registerCustomEvents(d *dispatcher.EventDispatcher) *dispatcher.EventDispatcher {
	routes := p.customEventRoutes()
	types := make([]string, 0, len(routes))
	for eventType := range routes {
		types = append(types, eventType)
	}
	// 排序仅为日志输出稳定。
	sort.Strings(types)
	for _, eventType := range types {
		route := routes[eventType]
		d = d.OnCustomizedEvent(eventType, func(ctx context.Context, req *larkevent.EventReq) error {
			p.dispatchCustomEvent(eventType, route, req)
			return nil
		})
		slog.Info(p.tag()+": subscribed custom event",
			"event_type", eventType, "handlers", len(route.handlers), "to_agent", route.toAgent)
	}
	return d
}

func (p *Platform) customEventRoutes() map[string]*customEventRoute {
	routes := map[string]*customEventRoute{}
	routeFor := func(eventType string) *customEventRoute {
		r := routes[eventType]
		if r == nil {
			r = &customEventRoute{}
			routes[eventType] = r
		}
		return r
	}
	for _, h := range p.eventHandlers {
		if h == nil {
			continue
		}
		for _, eventType := range h.EventTypes() {
			eventType = strings.TrimSpace(eventType)
			if eventType == "" {
				continue
			}
			r := routeFor(eventType)
			r.handlers = append(r.handlers, h)
		}
	}
	for eventType := range p.forwardToAgent {
		routeFor(eventType).toAgent = true
	}
	return routes
}

// dispatchCustomEvent 解析事件上下文并投递。SDK dispatcher 同步调用本函数，
// handler 插件与 agent 直转都在 goroutine 中执行，避免阻塞事件循环。
func (p *Platform) dispatchCustomEvent(eventType string, route *customEventRoute, req *larkevent.EventReq) {
	evt := parseCustomEvent(eventType, req.Body)
	if evt.EventID != "" && p.dedup.IsDuplicate("evt:"+evt.EventID) {
		slog.Debug(p.tag()+": duplicate custom event ignored", "event_type", eventType, "event_id", evt.EventID)
		return
	}
	// 注意：自定义事件不按进程启动时间过滤。飞书对未 ack 的事件会持续
	// 重推，重启后补送正是停机期间事件的送达保障；重复投递由 event_id
	// 去重兜底（与 IM 消息不同，事件没有"历史积压回放"语义）。

	for _, h := range route.handlers {
		go func(h caplarkevent.Handler) {
			if err := h.Handle(context.Background(), evt.Event); err != nil {
				slog.Error(p.tag()+": custom event handler failed",
					"event_type", eventType, "event_id", evt.EventID, "error", err)
			}
		}(h)
	}
	if route.toAgent {
		go p.forwardEventToAgent(evt)
	}
}

// customEvent 内部表示：caplarkevent.Event 加上仅平台使用的 header 字段。
type customEvent struct {
	caplarkevent.Event
	createTime time.Time
}

// parseCustomEvent 解析解密后的 schema 2.0 事件 JSON，提取 event_id、
// create_time 与常见的 chat/user 上下文路径。
func parseCustomEvent(eventType string, body []byte) customEvent {
	evt := customEvent{Event: caplarkevent.Event{Type: eventType, Raw: json.RawMessage(body)}}
	var envelope struct {
		Header struct {
			EventID    string `json:"event_id"`
			EventType  string `json:"event_type"`
			CreateTime string `json:"create_time"`
		} `json:"header"`
		Event json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		slog.Debug("feishu: custom event body is not schema 2.0 JSON", "event_type", eventType, "error", err)
		return evt
	}
	evt.EventID = envelope.Header.EventID
	if envelope.Header.EventType != "" {
		evt.Type = envelope.Header.EventType
	}
	if ms, err := strconv.ParseInt(envelope.Header.CreateTime, 10, 64); err == nil {
		evt.createTime = time.Unix(ms/1000, (ms%1000)*int64(time.Millisecond))
	}
	if len(envelope.Event) == 0 {
		return evt
	}
	var payload map[string]any
	if err := json.Unmarshal(envelope.Event, &payload); err != nil {
		return evt
	}
	evt.ChatID = firstString(payload,
		"chat_id",
		"message.chat_id",
		"chat.chat_id",
	)
	evt.UserID = firstString(payload,
		"sender.sender_id.open_id",
		"operator.operator_id.open_id",
		"operator_id.open_id",
		"user_id",
		"open_id",
	)
	if evt.UserID == "" {
		evt.UserID = firstSubscriberOpenID(payload)
	}
	return evt
}

// firstSubscriberOpenID 从 subscriber_ids 数组取第一个 open_id：妙记/会议等
// 用户身份订阅事件（minutes.minute.generated_v1、vc.*）用 subscriber_ids
// 携带订阅者（授权用户）上下文，用于路由到该用户的 p2p 会话。
func firstSubscriberOpenID(payload map[string]any) string {
	subs, ok := payload["subscriber_ids"].([]any)
	if !ok || len(subs) == 0 {
		return ""
	}
	first, ok := subs[0].(map[string]any)
	if !ok {
		return ""
	}
	openID, _ := first["open_id"].(string)
	return openID
}

// firstString 按顺序尝试点分路径，返回第一个命中的非空字符串。
func firstString(m map[string]any, paths ...string) string {
	for _, path := range paths {
		if v, ok := digString(m, strings.Split(path, ".")); ok {
			return v
		}
	}
	return ""
}

func digString(m map[string]any, segs []string) (string, bool) {
	cur := m
	for i, seg := range segs {
		v, ok := cur[seg]
		if !ok {
			return "", false
		}
		if i == len(segs)-1 {
			s, ok := v.(string)
			if ok && s != "" {
				return s, true
			}
			return "", false
		}
		next, ok := v.(map[string]any)
		if !ok {
			return "", false
		}
		cur = next
	}
	return "", false
}

// forwardEventToAgent 把事件直转 agent：按事件上下文路由会话——
// 带 chat_id 进对应群会话、只带 user_id 进 p2p 会话（回复走 open_id）、
// 都无则进平台专属 events 会话（只处理，无回复目标）。
func (p *Platform) forwardEventToAgent(evt customEvent) {
	chatID := evt.ChatID
	chatType := ""
	switch {
	case evt.ChatID != "":
		chatType = "group"
	case evt.UserID != "":
		chatID = evt.UserID
		chatType = "p2p"
	}
	channel := chatID
	if channel == "" {
		channel = "events"
	}
	sessionKey := string(rctx.BuildDeliverySessionID(p.tag(), channel, "", evt.UserID))

	content := fmt.Sprintf("[事件] %s\n%s", evt.Type, prettyEventJSON(evt.Raw))
	slog.Info(p.tag()+": forwarding custom event to agent",
		"event_type", evt.Type, "event_id", evt.EventID, "session_key", sessionKey)

	p.dispatchInbound(context.Background(), inboundMessage{
		sessionID: agentkit.SessionID(sessionKey),
		userID:    evt.UserID,
		content:   content,
		rctx:      replyContext{chatID: chatID, chatType: chatType, sessionKey: sessionKey},
	})
}

func prettyEventJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}
