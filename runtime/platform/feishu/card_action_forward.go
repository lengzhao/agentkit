package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func cardActionDedupKey(messageID, actionName, actionVal string, form map[string]interface{}) string {
	parts := []string{messageID, actionName, actionVal}
	if len(form) > 0 {
		b, _ := json.Marshal(form)
		parts = append(parts, string(b))
	}
	return common.InteractionDedupKey(parts...)
}

func formatUnknownCardActionMessage(cardText string, fetchedCard bool, action *callback.CallBackAction, actionVal, userID string) string {
	return common.FormatCardActionInbound(cardText, fetchedCard, formatCardActionOperation(action, actionVal), userID)
}

func formatCardActionOperation(action *callback.CallBackAction, actionVal string) string {
	if action == nil {
		if actionVal != "" {
			return "action=" + actionVal
		}
		return "(无操作详情)"
	}
	var lines []string
	if name := strings.TrimSpace(action.Name); name != "" {
		lines = append(lines, "控件: "+name)
	}
	if tag := strings.TrimSpace(action.Tag); tag != "" {
		lines = append(lines, "类型: "+tag)
	}
	if actionVal != "" {
		lines = append(lines, "action="+actionVal)
	} else if opt := strings.TrimSpace(action.Option); opt != "" {
		lines = append(lines, "option="+opt)
	}
	if iv := strings.TrimSpace(action.InputValue); iv != "" {
		lines = append(lines, "输入: "+iv)
	}
	if len(action.Options) > 0 {
		lines = append(lines, "选项: "+strings.Join(action.Options, ", "))
	}
	if action.Checked {
		lines = append(lines, "checked=true")
	}
	if len(action.Value) > 0 {
		lines = append(lines, "value: "+formatCallbackMap(action.Value))
	}
	if len(action.FormValue) > 0 {
		lines = append(lines, "表单:")
		lines = append(lines, indentLines(formatCallbackMap(action.FormValue), "  "))
	}
	if len(lines) == 0 {
		return "(无操作详情)"
	}
	return strings.Join(lines, "\n")
}

func formatCallbackMap(m map[string]interface{}) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+formatCallbackValue(m[k]))
	}
	return strings.Join(lines, "\n")
}

func formatCallbackValue(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprintf("%v", x)
	case []interface{}:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			parts = append(parts, formatCallbackValue(item))
		}
		return strings.Join(parts, ", ")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}

func indentLines(text, prefix string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (p *Platform) fetchCardMessageText(ctx context.Context, messageID string) (string, bool) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || p.client == nil {
		return "", false
	}
	req := larkim.NewGetMessageReqBuilder().
		MessageId(messageID).
		UserIdType(larkim.UserIdTypeOpenId).
		Build()

	var resp *larkim.GetMessageResp
	err := p.withTransientRetry(ctx, "get card message", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "get card message", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			var callErr error
			resp, callErr = client.Im.Message.Get(ctx, req, options...)
			if callErr != nil {
				return callErr
			}
			if !resp.Success() {
				return fmt.Errorf("%s: get message failed code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
	if err != nil {
		slog.Debug(p.tag()+": fetch card message for action failed", "message_id", messageID, "error", err)
		return "", false
	}
	if resp == nil || resp.Data == nil || len(resp.Data.Items) == 0 {
		return "", true
	}
	item := resp.Data.Items[0]
	if item == nil {
		return "", true
	}
	msgType := ""
	if item.MsgType != nil {
		msgType = *item.MsgType
	}
	content := ""
	if item.Body != nil && item.Body.Content != nil {
		content = *item.Body.Content
	}
	if content == "" {
		return "", true
	}
	text := p.extractHistoryText(msgType, content, item.Mentions)
	return strings.TrimSpace(text), true
}

func (p *Platform) forwardUnknownCardAction(ctx context.Context, event *callback.CardActionTriggerEvent, sessionKey, chatID, messageID, userID, actionVal string) (*callback.CardActionTriggerResponse, error) {
	if p.unknownCardAction != common.UnknownInteractionForward {
		return nil, nil
	}
	if !common.AllowList(p.allowFrom, userID) {
		return nil, nil
	}
	action := event.Event.Action
	actionName := ""
	if action != nil {
		actionName = action.Name
	}
	dedupKey := "card_action:" + cardActionDedupKey(messageID, actionName, actionVal, nil)
	if action != nil && len(action.FormValue) > 0 {
		dedupKey = "card_action:" + cardActionDedupKey(messageID, actionName, actionVal, action.FormValue)
	}
	if p.dedup.IsDuplicate(dedupKey) {
		slog.Debug(p.tag()+": duplicate card action ignored", "message_id", messageID, "action", actionVal)
		return cardActionAckToast(), nil
	}

	cardText, fetched := "", false
	if messageID != "" {
		cardText, fetched = p.fetchCardMessageText(ctx, messageID)
	}
	content := formatUnknownCardActionMessage(cardText, fetched, action, actionVal, userID)
	rctx := replyContext{messageID: messageID, chatID: chatID, sessionKey: sessionKey}

	slog.Info(p.tag()+": unknown card action forwarded to agent",
		"session_key", sessionKey, "user", userID, "action", actionVal, "fetched_card", fetched)

	go p.dispatchInbound(context.Background(), inboundMessage{
		sessionID: agentkit.SessionID(sessionKey),
		userID:    userID,
		content:   content,
		rctx:      rctx,
	})
	return cardActionAckToast(), nil
}

func cardActionAckToast() *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{
			Type:    "info",
			Content: "已提交",
		},
	}
}
