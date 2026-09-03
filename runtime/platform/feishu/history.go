package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit/cap/chathistory"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/session"
)

const maxHistoryContentLen = 4096

// ReadChatHistory lists messages from Feishu/Lark IM for the given delivery session.
func (p *Platform) ReadChatHistory(ctx context.Context, req chathistory.Request) (chathistory.Result, error) {
	parts := session.ParseDelivery(req.SessionID, req.UserID)
	if !parts.Routable || parts.Channel == "" {
		return chathistory.Result{}, fmt.Errorf("%s: chat_history requires a routable delivery session", p.tag())
	}
	if parts.Platform != "" && parts.Platform != p.platformTag {
		return chathistory.Result{}, fmt.Errorf("%s: session platform %q does not match %q", p.tag(), parts.Platform, p.platformTag)
	}
	if parts.Channel != "" && !common.AllowList(p.allowChat, parts.Channel) {
		return chathistory.Result{}, fmt.Errorf("%s: chat %q is not allowed", p.tag(), parts.Channel)
	}

	containerType := "chat"
	containerID := parts.Channel
	if req.Thread && parts.Thread != "" {
		containerType = "thread"
		containerID = parts.Thread
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	sortType := larkim.ReadHistoryMessageV1SortTypeByCreateTimeDesc
	switch strings.ToLower(strings.TrimSpace(req.Order)) {
	case "asc":
		sortType = larkim.ReadHistoryMessageV1SortTypeByCreateTimeAsc
	}

	builder := larkim.NewListMessageReqBuilder().
		ContainerIdType(containerType).
		ContainerId(containerID).
		PageSize(limit).
		SortType(sortType).
		WithSenderName(true).
		CardMsgContentType("raw_card_content")
	if token := strings.TrimSpace(req.Before); token != "" {
		builder = builder.PageToken(token)
	}
	if after := strings.TrimSpace(req.After); after != "" && containerType != "thread" {
		builder = builder.StartTime(after)
	}

	var resp *larkim.ListMessageResp
	err := p.withFreshTenantAccessTokenRetry(ctx, "list chat history", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
		var callErr error
		resp, callErr = client.Im.Message.List(ctx, builder.Build(), options...)
		return callErr
	})
	if err != nil {
		return chathistory.Result{}, fmt.Errorf("%s: list chat history: %w", p.tag(), err)
	}
	if resp == nil || !resp.Success() {
		code, msg := 0, ""
		if resp != nil {
			code, msg = resp.Code, resp.Msg
		}
		return chathistory.Result{}, fmt.Errorf("%s: list chat history code=%d msg=%s", p.tag(), code, msg)
	}

	messages := make([]chathistory.Message, 0)
	if resp.Data != nil {
		for _, item := range resp.Data.Items {
			if msg := p.historyMessageFromAPI(ctx, item); msg != nil {
				messages = append(messages, *msg)
			}
		}
	}

	result := chathistory.Result{
		Messages: messages,
		Source:   p.platformTag,
	}
	if resp.Data != nil {
		if resp.Data.HasMore != nil {
			result.HasMore = *resp.Data.HasMore
		}
		if resp.Data.PageToken != nil {
			result.Cursor = strings.TrimSpace(*resp.Data.PageToken)
		}
	}
	return result, nil
}

func (p *Platform) historyMessageFromAPI(ctx context.Context, item *larkim.Message) *chathistory.Message {
	if item == nil {
		return nil
	}
	if item.Deleted != nil && *item.Deleted {
		return nil
	}
	msgType := stringValue(item.MsgType)
	content := ""
	if item.Body != nil {
		content = strings.TrimSpace(stringValue(item.Body.Content))
	}
	if content == "" {
		return nil
	}

	text := p.extractHistoryText(msgType, content, item.Mentions)
	if text == "" {
		return nil
	}
	if len(text) > maxHistoryContentLen {
		text = text[:maxHistoryContentLen] + "…"
	}

	senderID := ""
	senderName := ""
	role := "user"
	if item.Sender != nil {
		senderID = stringValue(item.Sender.Id)
		senderName = strings.TrimSpace(stringValue(item.Sender.SenderName))
		switch stringValue(item.Sender.SenderType) {
		case "app":
			role = "assistant"
			if senderName == "" {
				senderName = p.resolveBotSenderName(senderID)
			}
		case "user":
			role = "user"
			if senderName == "" && senderID != "" {
				resolved := p.resolveUserName(senderID)
				if resolved != senderID {
					senderName = resolved
				}
			}
		}
	}
	if msgType == "system" {
		role = "system"
	}

	var ts int64
	if raw := stringValue(item.CreateTime); raw != "" {
		if ms, err := strconv.ParseInt(raw, 10, 64); err == nil {
			ts = ms / 1000
			if ts == 0 && ms > 0 {
				ts = ms
			}
		}
	}
	if ts == 0 {
		ts = time.Now().Unix()
	}

	return &chathistory.Message{
		ID:          stringValue(item.MessageId),
		SenderID:    senderID,
		SenderName:  senderName,
		Role:        role,
		Content:     text,
		Timestamp:   ts,
		ThreadID:    stringValue(item.ThreadId),
		MessageType: msgType,
	}
}

func (p *Platform) extractHistoryText(msgType, content string, mentions []*larkim.Mention) string {
	switch msgType {
	case "text":
		var textBody struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(content), &textBody); err == nil {
			return replaceMentions(textBody.Text, mentions)
		}
	case "post":
		return extractPostPlainText(content)
	case "interactive":
		return extractInteractiveCardText(content)
	default:
		return fmt.Sprintf("[%s]", msgType)
	}
	return ""
}
