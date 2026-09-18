package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

const recalledMessageTTL = 10 * time.Minute

func (p *Platform) markMessageRecalled(messageID string) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return
	}

	now := time.Now()
	p.recalledMu.Lock()
	defer p.recalledMu.Unlock()

	if p.recalledMsgIDs == nil {
		p.recalledMsgIDs = make(map[string]time.Time)
	}
	for id, markedAt := range p.recalledMsgIDs {
		if now.Sub(markedAt) > recalledMessageTTL {
			delete(p.recalledMsgIDs, id)
		}
	}
	p.recalledMsgIDs[messageID] = now
}

func (p *Platform) isMessageRecalled(messageID string) bool {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false
	}

	now := time.Now()
	p.recalledMu.Lock()
	defer p.recalledMu.Unlock()

	markedAt, ok := p.recalledMsgIDs[messageID]
	if !ok {
		return false
	}
	if now.Sub(markedAt) > recalledMessageTTL {
		delete(p.recalledMsgIDs, messageID)
		return false
	}
	return true
}

func isMessageWithdrawnCode(code int, msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	if code == 230011 {
		return true
	}
	for _, needle := range []string{"withdrawn", "recalled", "recall", "deleted", "not found", "not exist", "撤回"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func (p *Platform) IsMessageRecalled(ctx context.Context, rctx any) (bool, error) {
	rc, ok := rctx.(replyContext)
	if !ok || strings.TrimSpace(rc.messageID) == "" {
		return false, nil
	}
	messageID := strings.TrimSpace(rc.messageID)
	if p.isMessageRecalled(messageID) {
		return true, nil
	}
	if p.client == nil {
		return false, fmt.Errorf("%s: client not initialized", p.tag())
	}

	req := larkim.NewGetMessageReqBuilder().
		MessageId(messageID).
		UserIdType(larkim.UserIdTypeOpenId).
		Build()

	var resp *larkim.GetMessageResp
	if err := p.withTransientRetry(ctx, "get message", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "get message", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			var err error
			resp, err = client.Im.Message.Get(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: get message api call: %w", p.tag(), err)
			}
			if !resp.Success() {
				return fmt.Errorf("%s: get message failed code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	}); err != nil {
		if resp != nil && isMessageWithdrawnCode(resp.Code, resp.Msg) {
			p.markMessageRecalled(messageID)
			return true, nil
		}
		if isMessageWithdrawnError(err) {
			p.markMessageRecalled(messageID)
			return true, nil
		}
		return false, err
	}

	if resp == nil || resp.Data == nil || len(resp.Data.Items) == 0 {
		p.markMessageRecalled(messageID)
		return true, nil
	}
	for _, item := range resp.Data.Items {
		if item != nil && item.Deleted != nil && *item.Deleted {
			p.markMessageRecalled(messageID)
			return true, nil
		}
	}
	return false, nil
}

func isMessageWithdrawnError(err error) bool {
	if err == nil {
		return false
	}
	return isMessageWithdrawnCode(0, err.Error())
}

func (p *Platform) onMessageRecalled(_ context.Context, event *larkim.P2MessageRecalledV1) error {
	if event == nil || event.Event == nil {
		return nil
	}

	messageID := stringValue(event.Event.MessageId)
	chatID := stringValue(event.Event.ChatId)
	if messageID == "" {
		slog.Debug(p.tag()+": recall event without message id", "chat_id", chatID)
		return nil
	}
	if chatID != "" && !common.AllowList(p.allowChat, chatID) {
		slog.Debug(p.tag()+": recall event from unauthorized chat", "chat_id", chatID, "message_id", messageID)
		return nil
	}

	p.markMessageRecalled(messageID)
	slog.Info(p.tag()+": message recalled",
		"message_id", messageID,
		"chat_id", chatID,
		"recall_type", stringValue(event.Event.RecallType),
	)

	return nil
}
