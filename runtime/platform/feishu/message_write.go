package feishu

import (
	"context"
	"errors"
	"fmt"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// messageWriteError records write certainty independently of transport symptoms.
// In particular EOF, a timeout or a non-JSON gateway response can hide a commit.
type messageWriteError struct {
	Operation string
	Code      int
	Unknown   bool
	Cause     error
}

func (e *messageWriteError) Error() string {
	if e.Unknown {
		return fmt.Sprintf("%s: message write result unknown", e.Operation)
	}
	return fmt.Sprintf("%s: message write rejected (code=%d)", e.Operation, e.Code)
}
func (e *messageWriteError) Unwrap() error { return e.Cause }
func isMessageWriteUnknown(err error) bool {
	var write *messageWriteError
	return errors.As(err, &write) && write.Unknown
}

type messageWriteResult struct{ messageID, chatID string }

// writeMessage is the single IM Create/Reply boundary. Only a confirmed expired
// token is retried; no unknown write is repeated or moved into another target.
func (p *Platform) writeMessage(ctx context.Context, rc replyContext, msgType, content, operation string) (messageWriteResult, error) {
	var result messageWriteResult
	if err := ctx.Err(); err != nil {
		return result, &messageWriteError{Operation: operation, Cause: err}
	}
	if p.client == nil {
		return result, &messageWriteError{Operation: operation}
	}
	err := p.withFreshTenantAccessTokenRetry(ctx, operation, func(client *lark.Client, options ...larkcore.RequestOptionFunc) error {
		result = messageWriteResult{}
		var api *larkcore.ApiResp
		var code int
		var cause error
		if rc.messageID != "" {
			req := larkim.NewReplyMessageReqBuilder().MessageId(rc.messageID).Body(p.buildReplyMessageReqBody(rc, msgType, content)).Build()
			resp, err := client.Im.Message.Reply(ctx, req, options...)
			cause = err
			if resp != nil {
				api, code = resp.ApiResp, resp.Code
				if resp.Data != nil {
					result = messageWriteResult{stringValue(resp.Data.MessageId), stringValue(resp.Data.ChatId)}
				}
			}
		} else {
			req := larkim.NewCreateMessageReqBuilder().ReceiveIdType(receiveIDType(rc.chatID)).Body(larkim.NewCreateMessageReqBodyBuilder().ReceiveId(rc.chatID).MsgType(msgType).Content(content).Build()).Build()
			resp, err := client.Im.Message.Create(ctx, req, options...)
			cause = err
			if resp != nil {
				api, code = resp.ApiResp, resp.Code
				if resp.Data != nil {
					result = messageWriteResult{stringValue(resp.Data.MessageId), stringValue(resp.Data.ChatId)}
				}
			}
		}
		if cause == nil && api != nil && api.StatusCode >= 200 && api.StatusCode < 300 && code == 0 && result.messageID != "" {
			return nil
		}
		// HTTP status alone may come from a proxy after the upstream committed.
		rejected := false
		switch code {
		case 230001, 230002, 230006, 230013, 230020, 230035, 230050, 99991400, 99991401, 99991663:
			rejected = true
		}
		return &messageWriteError{Operation: operation, Code: code, Unknown: !rejected, Cause: cause}
	})
	return result, err
}
