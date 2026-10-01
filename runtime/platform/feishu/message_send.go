package feishu

import (
	"context"
	"fmt"
	"log/slog"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

func (p *Platform) sendNewMessageToChat(ctx context.Context, rc replyContext, msgType, content string) error {
	if rc.chatID == "" {
		return fmt.Errorf("%s: chatID is empty, cannot send new message", p.tag())
	}
	return p.createMessage(ctx, rc.chatID, msgType, content, "send")
}

func (p *Platform) buildReplyMessageReqBody(rc replyContext, msgType, content string) *larkim.ReplyMessageReqBody {
	body := larkim.NewReplyMessageReqBodyBuilder().
		MsgType(msgType).
		Content(content)
	if p.shouldReplyInThread(rc) {
		body.ReplyInThread(true)
	}
	return body.Build()
}

func (p *Platform) replyMessage(ctx context.Context, rc replyContext, msgType, content string) error {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(rc.messageID).
		Body(p.buildReplyMessageReqBody(rc, msgType, content)).
		Build()
	err := p.withTransientRetry(ctx, "reply", func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, "reply", func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Im.Message.Reply(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: reply api call: %w", p.tag(), err)
			}
			if !resp.Success() {
				return fmt.Errorf("%s: reply failed code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
			}
			return nil
		})
	})
	if err == nil {
		return nil
	}
	// 网关/权限故障（如 403）常同时拦截 Reply 与 Create，但 Create 走不同路径，
	// 多一次尝试就多一分送达最终答复的机会；chatID 为空时无法降级。
	if rc.chatID == "" {
		return err
	}
	slog.Warn(p.tag()+": reply api failed, falling back to create message",
		"chat_id", rc.chatID, "error", err)
	return p.createMessage(ctx, rc.chatID, msgType, content, "send (reply fallback)")
}

func (p *Platform) createMessage(ctx context.Context, chatID, msgType, content, op string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build()
	return p.withTransientRetry(ctx, op, func() error {
		return p.withFreshTenantAccessTokenRetry(ctx, op, func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
			resp, err := client.Im.Message.Create(ctx, req, options...)
			if err != nil {
				return fmt.Errorf("%s: %s api call: %w", p.tag(), op, err)
			}
			if !resp.Success() {
				return fmt.Errorf("%s: %s failed code=%d msg=%s", p.tag(), op, resp.Code, resp.Msg)
			}
			return nil
		})
	})
}
