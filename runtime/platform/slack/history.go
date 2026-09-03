package slack

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lengzhao/agentkit/cap/chathistory"
	"github.com/lengzhao/agentkit/runtime/session"
	slackapi "github.com/slack-go/slack"
)

const maxHistoryContentLen = 4096

// ReadChatHistory lists messages from a Slack channel or thread for the given delivery session.
func (p *Platform) ReadChatHistory(ctx context.Context, req chathistory.Request) (chathistory.Result, error) {
	if p.client == nil {
		return chathistory.Result{}, fmt.Errorf("slack: client not ready")
	}

	parts := session.ParseDelivery(req.SessionID, req.UserID)
	if !parts.Routable || parts.Channel == "" {
		return chathistory.Result{}, fmt.Errorf("slack: chat_history requires a routable delivery session")
	}
	if parts.Platform != "" && parts.Platform != "slack" {
		return chathistory.Result{}, fmt.Errorf("slack: session platform %q does not match slack", parts.Platform)
	}
	if !p.channelAllowed(parts.Channel, inferSlackChannelType(parts.Channel)) {
		return chathistory.Result{}, fmt.Errorf("slack: channel %q is not allowed", parts.Channel)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	asc := strings.EqualFold(strings.TrimSpace(req.Order), "asc")
	cursor := strings.TrimSpace(req.Before)
	oldest := slackOldestFromAfter(req.After)

	var (
		msgs     []slackapi.Message
		hasMore  bool
		nextPage string
		err      error
	)
	if req.Thread && parts.Thread != "" {
		msgs, hasMore, nextPage, err = p.client.GetConversationRepliesContext(ctx, &slackapi.GetConversationRepliesParameters{
			ChannelID: parts.Channel,
			Timestamp: parts.Thread,
			Cursor:    cursor,
			Limit:     limit,
			Oldest:    oldest,
		})
	} else {
		resp, histErr := p.client.GetConversationHistoryContext(ctx, &slackapi.GetConversationHistoryParameters{
			ChannelID: parts.Channel,
			Cursor:    cursor,
			Limit:     limit,
			Oldest:    oldest,
		})
		if histErr != nil {
			err = histErr
		} else if resp != nil {
			msgs = resp.Messages
			hasMore = resp.HasMore
			nextPage = resp.ResponseMetaData.NextCursor
		}
	}
	if err != nil {
		return chathistory.Result{}, fmt.Errorf("slack: list chat history: %w", err)
	}

	messages := make([]chathistory.Message, 0, len(msgs))
	for _, item := range msgs {
		if msg := historyMessageFromSlack(item); msg != nil {
			messages = append(messages, *msg)
		}
	}
	if asc {
		reverseHistoryMessages(messages)
	}

	return chathistory.Result{
		Messages: messages,
		HasMore:  hasMore,
		Cursor:   nextPage,
		Source:   "slack",
	}, nil
}

func historyMessageFromSlack(item slackapi.Message) *chathistory.Message {
	if item.Hidden {
		return nil
	}
	switch item.SubType {
	case slackapi.MsgSubTypeMessageDeleted, slackapi.MsgSubTypeMessageChanged:
		return nil
	}

	text := strings.TrimSpace(item.Text)
	if text == "" {
		text = historyFallbackText(item)
	}
	if text == "" {
		return nil
	}
	if len(text) > maxHistoryContentLen {
		text = text[:maxHistoryContentLen] + "…"
	}

	role := "user"
	senderID := strings.TrimSpace(item.User)
	senderName := strings.TrimSpace(item.Username)
	if senderID == "" && strings.TrimSpace(item.BotID) != "" {
		role = "assistant"
		senderID = strings.TrimSpace(item.BotID)
		if senderName == "" && item.BotProfile != nil {
			senderName = strings.TrimSpace(item.BotProfile.Name)
		}
	}
	if item.SubType == slackapi.MsgSubTypeBotMessage {
		role = "assistant"
	}
	if senderName == "" {
		senderName = senderID
	}

	msgType := strings.TrimSpace(item.Type)
	if sub := strings.TrimSpace(item.SubType); sub != "" {
		msgType = sub
	}

	threadID := strings.TrimSpace(item.ThreadTimestamp)

	ts := slackTimestampUnix(item.Timestamp)
	return &chathistory.Message{
		ID:          strings.TrimSpace(item.Timestamp),
		SenderID:    senderID,
		SenderName:  senderName,
		Role:        role,
		Content:     text,
		Timestamp:   ts,
		ThreadID:    threadID,
		MessageType: msgType,
	}
}

func historyFallbackText(item slackapi.Message) string {
	if sub := strings.TrimSpace(item.SubType); sub != "" {
		switch sub {
		case slackapi.MsgSubTypeFileShare:
			if len(item.Files) > 0 {
				names := make([]string, 0, len(item.Files))
				for _, f := range item.Files {
					if name := strings.TrimSpace(f.Name); name != "" {
						names = append(names, name)
					}
				}
				if len(names) > 0 {
					return "[file_share: " + strings.Join(names, ", ") + "]"
				}
			}
			return "[file_share]"
		default:
			return "[" + sub + "]"
		}
	}
	if len(item.Files) > 0 {
		return "[file]"
	}
	return ""
}

func slackTimestampUnix(ts string) int64 {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return 0
	}
	if idx := strings.IndexByte(ts, '.'); idx > 0 {
		ts = ts[:idx]
	}
	v, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func slackOldestFromAfter(after string) string {
	after = strings.TrimSpace(after)
	if after == "" {
		return ""
	}
	if strings.Contains(after, ".") {
		return after
	}
	if _, err := strconv.ParseInt(after, 10, 64); err == nil {
		return after + ".000000"
	}
	return after
}

func reverseHistoryMessages(messages []chathistory.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}
