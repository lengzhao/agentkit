package feishu

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/ingress"
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

	dispatch := func() {
		action := ingress.Skip
		if route.toAgent {
			action = ingress.Submit
		}
		plan := p.evaluateIngress(context.Background(), ingress.Input{Platform: p.tag(), InstanceID: p.PlatformInstance(), Kind: ingress.Event, Type: evt.Type, ID: evt.EventID, ChatID: evt.ChatID, UserID: evt.UserID, ConversationKind: "unknown", Context: ingress.Unknown, Default: ingress.Plan{Agent: action, Handlers: len(route.handlers) > 0}, Raw: append([]byte(nil), evt.Raw...)})

		if plan.Handlers {
			for _, h := range route.handlers {
				go func(h caplarkevent.Handler) {
					if err := h.Handle(context.Background(), evt.Event); err != nil {
						slog.Error(p.tag()+": custom event handler failed",
							"event_type", eventType, "event_id", evt.EventID, "error", err)
					}
				}(h)
			}
		}
		if plan.Agent == ingress.Submit {
			go p.forwardEventToAgent(evt)
		}
	}
	if p.inboundPolicy != nil {
		go dispatch()
	} else {
		dispatch()
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

// forwardEventSessionKey 为直转 agent 的自定义事件分配独立 session，避免同群/同用户
// 多次事件共享历史。回复目标仍由 replyContext 的 chat_id / open_id 决定。
func forwardEventSessionKey(platformTag string, evt customEvent) string {
	id := strings.TrimSpace(evt.EventID)
	if id == "" {
		sum := sha256.Sum256(evt.Raw)
		id = hex.EncodeToString(sum[:8])
	}
	return string(rctx.BuildDeliverySessionID(platformTag, "evt:"+id, "", evt.UserID))
}

// forwardEventToAgent 把事件直转 agent：每条事件使用独立 session。
// DM 话题模式下先创建根消息，并使用常规话题 session key 以支持用户续接。
// 回复按事件上下文路由——带 chat_id 回群、仅 user_id 时回 p2p（open_id）、都无则无回复目标。
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
	sessionKey := forwardEventSessionKey(p.tag(), evt)
	rc := replyContext{chatID: chatID, chatType: chatType, sessionKey: sessionKey}
	if p.cfg.DMThreadMode && chatType == "p2p" {
		root, err := p.createEventThreadRoot(context.Background(), evt)
		if err != nil {
			slog.Warn(p.tag()+": event topic creation failed, forwarding without topic", "event_id", evt.EventID, "error", err)
		} else {
			chatID = *root.ChatId
			sessionKey = string(rctx.BuildDeliverySessionID(p.tag(), chatID, *root.MessageId, evt.UserID))
			rc = replyContext{chatID: chatID, chatType: chatType, messageID: *root.MessageId, sessionKey: sessionKey}
		}
	}

	content := fmt.Sprintf("[事件] %s\n%s", evt.Type, prettyEventJSON(evt.Raw))
	slog.Info(p.tag()+": forwarding custom event to agent",
		"event_type", evt.Type, "event_id", evt.EventID, "session_key", sessionKey)

	p.dispatchInbound(context.Background(), inboundMessage{
		sessionID: agentkit.SessionID(sessionKey),
		userID:    evt.UserID,
		content:   content,
		rctx:      rc,
	})
}

func prettyEventJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

// createEventThreadRoot gives a user-only event a durable IM anchor. Using the
// normal DM topic delivery key lets subsequent user replies resume this session,
// including after a restart, without an event-to-topic lookup table.
func (p *Platform) createEventThreadRoot(ctx context.Context, evt customEvent) (*larkim.CreateMessageRespData, error) {
	content, err := json.Marshal(map[string]string{"text": "[事件] " + evt.Type})
	if err != nil {
		return nil, err
	}
	// Stable UUID avoids duplicate roots when a transient failure retries Create.
	uuid := fmt.Sprintf("agentkit-event-%x", sha256.Sum256([]byte(forwardEventSessionKey(p.tag(), evt))))[:46]
	req := larkim.NewCreateMessageReqBuilder().ReceiveIdType("open_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().ReceiveId(evt.UserID).
			MsgType(larkim.MsgTypeText).Content(string(content)).Uuid(uuid).Build()).Build()
	var root *larkim.CreateMessageRespData
	err = p.withTransientRetry(ctx, "create event topic", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "create event topic", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Im.Message.Create(ctx, req, options...)
			if err != nil {
				return err
			}
			if !resp.Success() {
				return fmt.Errorf("create event topic failed code=%d msg=%s", resp.Code, resp.Msg)
			}
			if resp.Data == nil || resp.Data.MessageId == nil || *resp.Data.MessageId == "" || resp.Data.ChatId == nil || *resp.Data.ChatId == "" {
				return fmt.Errorf("create event topic returned no message_id or chat_id")
			}
			root = resp.Data
			return nil
		})
	})
	return root, err
}
