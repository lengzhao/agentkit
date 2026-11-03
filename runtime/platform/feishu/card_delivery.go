package feishu

import (
	"context"
	"net/http"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// SendCard posts a custom plugin card using the existing Lark/Feishu client.
func (p *Platform) SendCard(ctx context.Context, route agentkit.RouteRef, card *cap.Card) (cap.MessageRef, error) {
	route, target, err := common.CardRoute(ctx, route, card, p.platformTag, p.PlatformInstance())
	if err != nil {
		return cap.MessageRef{}, err
	}
	if p.client == nil {
		return cap.MessageRef{}, &cap.Error{Code: cap.CodeFailed}
	}
	if !common.AllowList(p.allowChat, target.ChannelID) {
		return cap.MessageRef{}, &cap.Error{Code: cap.CodeForbidden}
	}
	if target.ThreadID != "" {
		// Reply takes only a message ID: verify the chat before sending into a thread.
		resp, err := p.client.Im.Message.Get(ctx, larkim.NewGetMessageReqBuilder().MessageId(target.ThreadID).Build())
		if resp == nil {
			return cap.MessageRef{}, &cap.Error{Code: cap.CodeTransport, Cause: err}
		}
		if err = cardAPIError(resp.ApiResp, resp.Code, err, false); err != nil {
			return cap.MessageRef{}, err
		}
		verified := false
		if resp.Data != nil {
			for _, item := range resp.Data.Items {
				if item != nil && stringValue(item.MessageId) == target.ThreadID && stringValue(item.ChatId) == target.ChannelID && (item.Deleted == nil || !*item.Deleted) {
					verified = true
					if root := stringValue(item.RootId); root != "" {
						target.ThreadID = root
					}
					break
				}
			}
		}
		if !verified {
			return cap.MessageRef{}, &cap.Error{Code: cap.CodeInvalidTarget}
		}
	}
	session, _ := rctx.RouteSessionID(route)
	content := renderCard(card, string(session))
	var api *larkcore.ApiResp
	var code int
	var callErr error
	var id, chat string
	if target.ThreadID != "" {
		resp, err := p.client.Im.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().MessageId(target.ThreadID).Body(larkim.NewReplyMessageReqBodyBuilder().MsgType("interactive").Content(content).ReplyInThread(true).Build()).Build())
		callErr = err
		if resp != nil {
			api, code = resp.ApiResp, resp.Code
			if resp.Data != nil {
				id, chat = stringValue(resp.Data.MessageId), stringValue(resp.Data.ChatId)
			}
		}
	} else {
		resp, err := p.client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().ReceiveIdType(receiveIDType(target.ChannelID)).Body(larkim.NewCreateMessageReqBodyBuilder().ReceiveId(target.ChannelID).MsgType("interactive").Content(content).Build()).Build())
		callErr = err
		if resp != nil {
			api, code = resp.ApiResp, resp.Code
			if resp.Data != nil {
				id, chat = stringValue(resp.Data.MessageId), stringValue(resp.Data.ChatId)
			}
		}
	}
	if err = cardAPIError(api, code, callErr, true); err != nil {
		return cap.MessageRef{}, err
	}
	if id == "" || chat == "" {
		return cap.MessageRef{}, &cap.Error{Code: cap.CodeFailed, Unknown: true}
	}
	return cap.MessageRef{ID: id, Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: p.platformTag, Instance: route.Instance, ChannelID: chat, ThreadID: target.ThreadID})}, nil
}

func cardAPIError(resp *larkcore.ApiResp, code int, cause error, writing bool) error {
	out := &cap.Error{Code: cap.CodeTransport, Unknown: writing, Cause: cause}
	if resp == nil {
		return out
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || code == 99991400 || code == 99991401 || code == 230020:
		out.Code = cap.CodeRateLimited
		out.Unknown = false
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || code == 230002 || code == 230006 || code == 230013 || code == 230035 || code == 230050:
		out.Code = cap.CodeForbidden
		out.Unknown = false
	case cause != nil || resp.StatusCode >= 500:
		return out
	case code == 0 && resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case code == 230001:
		out.Code = cap.CodeInvalidTarget
		out.Unknown = false
	default:
		out.Code = cap.CodeFailed
	}
	return out
}

var _ cap.CardSender = (*Platform)(nil)
