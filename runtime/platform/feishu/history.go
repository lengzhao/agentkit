package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit/cap/chathistory"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

const maxHistoryContentLen = 4096

// ReadChatHistory lists messages from Feishu/Lark IM for the given delivery session.
func (p *Platform) ReadChatHistory(ctx context.Context, req chathistory.Request) (chathistory.Result, error) {
	parts, ok := rctx.DecodeSessionRoute(req.Route)
	if !ok || parts.ChannelID == "" {
		return chathistory.Result{}, fmt.Errorf("%s: chat_history requires a routable delivery session; hint: pass sessionId explicitly (e.g. \"lark:oc_xxx\") or a userId", p.tag())
	}
	if req.Route.Platform != "" && req.Route.Platform != p.platformTag {
		return chathistory.Result{}, fmt.Errorf("%s: session platform %q does not match %q; hint: check the sessionId prefix matches the target platform", p.tag(), req.Route.Platform, p.platformTag)
	}
	if parts.ChannelID != "" && !common.AllowList(p.allowChat, parts.ChannelID) {
		return chathistory.Result{}, fmt.Errorf("%s: chat %q is not in the allowlist; hint: ask an admin to allow this chat, or read a chat that is already allowed", p.tag(), parts.ChannelID)
	}

	// chainRoot 非空表示需要限定到某条回复链/话题：session 的 thread 段存的是
	// 根消息 ID（om_...）。飞书里一条链可能有两种形态——纯回复链（消息混在
	// chat 容器里，按 root_id 过滤），或被转成话题（后续消息进 omt_ thread
	// 容器）；先聊几轮再转话题时两部分都存在，需要合并。
	containerType := "chat"
	containerID := parts.ChannelID
	chainRoot := ""
	if req.Thread && parts.ThreadID != "" {
		chainRoot = strings.TrimSpace(parts.ThreadID)
		if threadID := p.resolveThreadID(ctx, chainRoot); threadID != "" {
			containerType = "thread"
			containerID = threadID
		}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	after := strings.TrimSpace(req.After)

	items, hasMore, cursor, err := p.listHistoryItems(ctx, containerType, containerID, limit, req.Order, req.Before, after)
	if err != nil {
		if containerType == "thread" {
			return chathistory.Result{}, fmt.Errorf("%w; hint: the topic container may be unreadable, retry with thread=false to read the whole chat instead", err)
		}
		return chathistory.Result{}, fmt.Errorf("%w; hint: retry with a smaller limit, or pass before=<cursor> to page, or thread=false for whole-chat history", err)
	}

	var chainItems []*larkim.Message
	if chainRoot != "" {
		if containerType == "chat" {
			// 纯回复链：从群消息里滤出该链。
			items = filterChainItems(items, chainRoot)
		} else {
			// 话题：转话题前的回复仍在群容器中，捞出后合并。
			chatItems, _, _, chatErr := p.listHistoryItems(ctx, "chat", parts.ChannelID, limit, req.Order, "", after)
			if chatErr != nil {
				slog.Warn(p.tag()+": read pre-topic chain failed, keep topic messages only", "root_message_id", chainRoot, "error", chatErr)
			} else {
				chainItems = filterChainItems(chatItems, chainRoot)
			}
		}
	}

	messages := make([]chathistory.Message, 0, len(items)+len(chainItems))
	seen := make(map[string]struct{}, len(items)+len(chainItems))
	for _, item := range append(items, chainItems...) {
		msg := p.historyMessageFromAPI(ctx, item)
		if msg == nil {
			continue
		}
		if _, dup := seen[msg.ID]; dup {
			continue
		}
		seen[msg.ID] = struct{}{}
		messages = append(messages, *msg)
	}
	if len(chainItems) > 0 {
		sortHistoryMessages(messages, req.Order)
		if len(messages) > limit {
			messages = messages[:limit]
		}
	}

	return chathistory.Result{
		Messages: messages,
		HasMore:  hasMore,
		Cursor:   cursor,
		Source:   p.platformTag,
	}, nil
}

// listHistoryItems fetches one page of messages from a chat/thread container.
func (p *Platform) listHistoryItems(ctx context.Context, containerType, containerID string, limit int, order, pageToken, startTime string) ([]*larkim.Message, bool, string, error) {
	sortType := larkim.ReadHistoryMessageV1SortTypeByCreateTimeDesc
	if strings.EqualFold(strings.TrimSpace(order), "asc") {
		sortType = larkim.ReadHistoryMessageV1SortTypeByCreateTimeAsc
	}

	builder := larkim.NewListMessageReqBuilder().
		ContainerIdType(containerType).
		ContainerId(containerID).
		PageSize(limit).
		SortType(sortType).
		WithSenderName(true).
		CardMsgContentType("raw_card_content")
	if token := strings.TrimSpace(pageToken); token != "" {
		builder = builder.PageToken(token)
	}
	// thread 容器不支持 start_time。
	if startTime != "" && containerType != "thread" {
		builder = builder.StartTime(startTime)
	}

	var resp *larkim.ListMessageResp
	err := p.withFreshTenantAccessTokenRetry(ctx, "list chat history", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
		var callErr error
		resp, callErr = client.Im.Message.List(ctx, builder.Build(), options...)
		return callErr
	})
	if err != nil {
		return nil, false, "", fmt.Errorf("%s: list chat history: %w", p.tag(), err)
	}
	if resp == nil || !resp.Success() {
		code, msg := 0, ""
		if resp != nil {
			code, msg = resp.Code, resp.Msg
		}
		return nil, false, "", fmt.Errorf("%s: list chat history code=%d msg=%s", p.tag(), code, msg)
	}

	if resp.Data == nil {
		return nil, false, "", nil
	}
	return resp.Data.Items, resp.Data.HasMore != nil && *resp.Data.HasMore, strings.TrimSpace(stringValue(resp.Data.PageToken)), nil
}

// filterChainItems keeps the root message itself and its direct/indirect replies.
func filterChainItems(items []*larkim.Message, rootMessageID string) []*larkim.Message {
	out := make([]*larkim.Message, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if stringValue(item.MessageId) == rootMessageID || stringValue(item.RootId) == rootMessageID {
			out = append(out, item)
		}
	}
	return out
}

// sortHistoryMessages orders merged messages by timestamp; default desc.
func sortHistoryMessages(messages []chathistory.Message, order string) {
	desc := !strings.EqualFold(strings.TrimSpace(order), "asc")
	sort.SliceStable(messages, func(i, j int) bool {
		if desc {
			return messages[i].Timestamp > messages[j].Timestamp
		}
		return messages[i].Timestamp < messages[j].Timestamp
	})
}

// resolveThreadID maps a session thread segment to a Feishu topic thread ID
// (omt_...). The segment may already be a topic ID; otherwise it is a root
// message ID (om_...) and is resolved via Message.Get. Returns "" when the
// message is not part of a topic thread or lookup fails.
func (p *Platform) resolveThreadID(ctx context.Context, segment string) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return ""
	}
	if strings.HasPrefix(segment, "omt_") {
		return segment
	}
	if !strings.HasPrefix(segment, "om_") {
		return ""
	}

	req := larkim.NewGetMessageReqBuilder().
		MessageId(segment).
		UserIdType(larkim.UserIdTypeOpenId).
		Build()

	var resp *larkim.GetMessageResp
	err := p.withFreshTenantAccessTokenRetry(ctx, "get message thread", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
		var callErr error
		resp, callErr = client.Im.Message.Get(ctx, req, options...)
		return callErr
	})
	if err != nil {
		slog.Warn(p.tag()+": resolve thread id failed, fallback to chat history", "root_message_id", segment, "error", err)
		return ""
	}
	if resp == nil || !resp.Success() || resp.Data == nil {
		slog.Warn(p.tag()+": resolve thread id rejected, fallback to chat history", "root_message_id", segment)
		return ""
	}
	for _, item := range resp.Data.Items {
		if threadID := stringValue(item.ThreadId); strings.HasPrefix(threadID, "omt_") {
			return threadID
		}
	}
	// Plain reply chain without a Feishu topic: no thread container exists.
	return ""
}

func (p *Platform) historyMessageFromAPI(_ context.Context, item *larkim.Message) *chathistory.Message {
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
