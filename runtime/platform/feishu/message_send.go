package feishu

import (
	"context"
	"fmt"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
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
	_, err := p.writeMessage(ctx, rc, msgType, content, "reply")
	return err
}

// receiveIDType 按 id 前缀选择 receive_id_type：ou_ 前缀是用户 open_id
// （自定义事件直转 agent 的 p2p 会话回复），其余按 chat_id。
func receiveIDType(id string) string {
	if strings.HasPrefix(id, "ou_") {
		return "open_id"
	}
	return "chat_id"
}

func (p *Platform) createMessage(ctx context.Context, chatID, msgType, content, op string) error {
	_, err := p.writeMessage(ctx, replyContext{chatID: chatID}, msgType, content, op)
	return err
}
