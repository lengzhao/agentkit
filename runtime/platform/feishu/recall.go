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
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
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

	// 异步派发 /stop：busy session 解析可能读 SessionStore，
	// 不应阻塞 SDK 事件分发 goroutine（同 onMessage 的异步惯例）。
	if p.stopOnRecall {
		go p.cancelTurnsForRecalledMessage(messageID)
	}

	return nil
}

// cancelTurnsForRecalledMessage 撤回触发（或 steer 进）进行中 turn 的消息时，
// 对该 session 派发 /stop 取消当前 turn。仅匹配进行中 turn 关联的消息
// （turn reaction batch，含 trigger 与 steer 消息）；撤回无关旧消息不影响
// 正在执行的 turn。
func (p *Platform) cancelTurnsForRecalledMessage(messageID string) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || p.commands == nil {
		return
	}
	p.turnReactions.Range(func(key, value any) bool {
		sessionID, ok := key.(agentkit.SessionID)
		if !ok {
			return true
		}
		batch, ok := value.(*turnReactionBatch)
		if !ok {
			return true
		}
		rc, hit := batch.findActive(messageID)
		if !hit {
			return true
		}
		p.stopSessionForRecall(sessionID, rc)
		return true
	})
}

// stopSessionForRecall 以撤回消息所在 session 的路由上下文派发 /stop。
// /stop 内部的 busy session 解析会按 delivery、scope 与 active 子 session
// 等候选 id 校验 IsSessionBusy，turn 已结束时为无操作。撤回是平台级自动化
// 而非用户输入的 slash，因此不受 disablePlatformSlash 影响。
func (p *Platform) stopSessionForRecall(sessionID agentkit.SessionID, rc replyContext) {
	route := rctx.BuildSessionRoute(agentkit.SessionRouteInput{
		Platform:   p.platformTag,
		DeliveryID: sessionID,
		ChannelID:  rc.chatID,
		ThreadID:   threadIDFromSessionKey(string(sessionID)),
	})
	outcome, err := common.ProcessSlash(context.Background(), p.commands, common.SlashContext{
		Route:        route,
		SessionScope: p.sessionScope,
	}, "/stop")
	if err != nil {
		slog.Warn(p.tag()+": recall stop dispatch failed",
			"session_id", sessionID,
			"message_id", rc.messageID,
			"error", err,
		)
		return
	}
	slog.Info(p.tag()+": turn stop dispatched by message recall",
		"session_id", sessionID,
		"message_id", rc.messageID,
		"reply", outcome.Reply,
	)
}
