package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func (p *Platform) messageTarget(ctx context.Context, route agentkit.RouteRef) (agentkit.RouteRef, agentkit.SessionRouteTarget, error) {
	route, target, err := common.MessageRoute(ctx, route, p.platformTag, p.PlatformInstance())
	if err != nil {
		return route, target, err
	}
	if p.client == nil {
		return route, target, &cap.Error{Code: cap.CodeFailed, Outcome: cap.OutcomeNotSent}
	}
	if !common.AllowList(p.allowChat, target.ChannelID) {
		return route, target, &cap.Error{Code: cap.CodeForbidden, Outcome: cap.OutcomeNotSent}
	}
	return route, target, nil
}
func larkDeliveryError(resp *larkcore.ApiResp, code int, err error) error {
	// A proxy/transport failure or malformed success may hide an accepted write.
	out := &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeUnknown, Cause: err}
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	if status == http.StatusTooManyRequests || code == 99991400 || code == 99991401 || code == 230020 {
		out.Code = cap.CodeRateLimited
		out.Outcome = cap.OutcomeNotSent
		if resp != nil {
			n, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			out.RetryAfter = time.Duration(n) * time.Second
		}
		return out
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		out.Code = cap.CodeForbidden
		out.Outcome = cap.OutcomeNotSent
		return out
	}
	if err != nil || status >= 500 {
		return out
	}
	if code == 0 && status >= 200 && status < 300 {
		return nil
	}
	out.Code = cap.CodeFailed
	switch code {
	case 230001:
		out.Code = cap.CodeInvalidTarget
		out.Outcome = cap.OutcomeNotSent
	case 230002, 230006, 230013, 230035, 230050, 99991663, 99991664, 99991672:
		out.Code = cap.CodeForbidden
		out.Outcome = cap.OutcomeNotSent
	}
	return out
}
func larkMessageBody(parts []cap.Part) string {
	// Structured post nodes keep mentions out of caller-supplied markup and avoid
	// leaking Lark XML into the generic content contract.
	row := make([]map[string]string, 0, len(parts))
	for _, part := range parts {
		if part.Type == "mention" {
			row = append(row, map[string]string{"tag": "at", "user_id": part.UserID})
		} else {
			row = append(row, map[string]string{"tag": "text", "text": part.Text})
		}
	}
	raw, _ := json.Marshal(map[string]any{"zh_cn": map[string]any{"content": [][]map[string]string{row}}})
	return string(raw)
}
func (p *Platform) Publish(ctx context.Context, req cap.PublishRequest) (cap.Receipt, error) {
	return p.publishBusiness(ctx, req, false)
}
func (p *Platform) CreateTopic(ctx context.Context, req cap.PublishRequest) (cap.Receipt, error) {
	return p.publishBusiness(ctx, req, true)
}
func (p *Platform) publishBusiness(ctx context.Context, req cap.PublishRequest, topic bool) (cap.Receipt, error) {
	receipt := common.MessageReceipt(req.Route)
	route, target, err := p.messageTarget(ctx, req.Route)
	if err != nil {
		return receipt, err
	}
	receipt.Route = route
	req.Route = route
	if topic && target.ThreadID != "" {
		return receipt, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
	}
	chunks, err := common.SplitMessageContent(req.Content)
	if err != nil {
		return receipt, err
	}
	if target.ThreadID != "" {
		parentRoute := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: p.platformTag, Instance: route.Instance, ChannelID: target.ChannelID})
		parent, readErr := p.ReadMessage(ctx, cap.ReadRequest{Message: cap.MessageRef{Route: parentRoute, ID: target.ThreadID}, Caller: req.Caller, OperationID: req.OperationID})
		if readErr != nil {
			return receipt, larkUnsentError(readErr)
		}
		if actual, ok := rctx.DecodeSessionRoute(parent.Ref.Route); ok && actual.ThreadID != "" {
			target.ThreadID = actual.ThreadID
		}
		receipt.Route = rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: p.platformTag, Instance: route.Instance, ChannelID: target.ChannelID, ThreadID: target.ThreadID})
	}
	if req.OperationID != "" {
		receipt.Deduplication = cap.Deduplication{Mode: "native", Window: time.Hour}
	}
	operation := "publish"
	if topic {
		operation = "create_topic"
	}
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return common.FailMessagePart(receipt, i, &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeNotSent, Cause: err})
		}
		// Hash against the original target, so topic-root creation and subsequent
		// parts keep the same IDs across restart/retry.
		key := common.MessageOperationKey(req, operation, i)
		var id, chatID string
		if target.ThreadID != "" {
			// Explicit thread writes do not use the legacy reply->chat fallback.
			builder := larkim.NewReplyMessageReqBodyBuilder().MsgType("post").Content(larkMessageBody(chunk)).ReplyInThread(true)
			if key != "" {
				builder.Uuid(key)
			}
			resp, callErr := p.client.Im.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().MessageId(target.ThreadID).Body(builder.Build()).Build())
			if resp == nil {
				return common.FailMessagePart(receipt, i, &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeUnknown, Cause: callErr})
			}
			if err = larkDeliveryError(resp.ApiResp, resp.Code, callErr); err != nil {
				return common.FailMessagePart(receipt, i, err)
			}
			if resp.Data != nil {
				id = stringValue(resp.Data.MessageId)
				chatID = stringValue(resp.Data.ChatId)
			}
		} else {
			builder := larkim.NewCreateMessageReqBodyBuilder().ReceiveId(target.ChannelID).MsgType("post").Content(larkMessageBody(chunk))
			if key != "" {
				builder.Uuid(key)
			}
			resp, callErr := p.client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().ReceiveIdType(receiveIDType(target.ChannelID)).Body(builder.Build()).Build())
			if resp == nil {
				return common.FailMessagePart(receipt, i, &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeUnknown, Cause: callErr})
			}
			if err = larkDeliveryError(resp.ApiResp, resp.Code, callErr); err != nil {
				return common.FailMessagePart(receipt, i, err)
			}
			if resp.Data != nil {
				id = stringValue(resp.Data.MessageId)
				chatID = stringValue(resp.Data.ChatId)
			}
		}
		if id == "" || chatID == "" {
			return common.FailMessagePart(receipt, i, &cap.Error{Code: cap.CodeFailed, Outcome: cap.OutcomeUnknown})
		}
		actual := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: p.platformTag, Instance: route.Instance, ChannelID: chatID, ThreadID: target.ThreadID})
		receipt.Messages = append(receipt.Messages, cap.MessageRef{Route: actual, ID: id})
		if topic && i == 0 {
			target.ThreadID = id
			target.ChannelID = chatID
			receipt.Route = rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: p.platformTag, Instance: route.Instance, ChannelID: chatID, ThreadID: id})
		} else if !topic {
			receipt.Route = actual
		}
	}
	receipt.Outcome = cap.OutcomeConfirmed
	return receipt, nil
}
func (p *Platform) ReadMessage(ctx context.Context, req cap.ReadRequest) (cap.Message, error) {
	ref := req.Message
	route, target, err := p.messageTarget(ctx, ref.Route)
	if err != nil {
		return cap.Message{}, err
	}
	ref.Route = route
	if ref.ID == "" {
		return cap.Message{}, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
	}
	resp, callErr := p.client.Im.Message.Get(ctx, larkim.NewGetMessageReqBuilder().MessageId(ref.ID).UserIdType("open_id").Build())
	if resp == nil {
		return cap.Message{}, &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeUnknown, Cause: callErr}
	}
	if err = larkDeliveryError(resp.ApiResp, resp.Code, callErr); err != nil {
		return cap.Message{}, err
	}
	if resp.Data != nil {
		for _, item := range resp.Data.Items {
			if item == nil || stringValue(item.MessageId) != ref.ID || stringValue(item.ChatId) != target.ChannelID || (item.Deleted != nil && *item.Deleted) {
				continue
			}
			if target.ThreadID != "" && target.ThreadID != ref.ID && target.ThreadID != stringValue(item.RootId) && target.ThreadID != stringValue(item.ThreadId) {
				continue
			}
			if root := stringValue(item.RootId); root != "" {
				ref.Route = rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: p.platformTag, Instance: route.Instance, ChannelID: target.ChannelID, ThreadID: root})
			}
			content := ""
			if item.Body != nil {
				content = p.extractHistoryText(stringValue(item.MsgType), stringValue(item.Body.Content), item.Mentions)
			}
			return cap.Message{Ref: ref, Content: []cap.Part{{Type: "text", Text: content}}}, nil
		}
	}
	return cap.Message{}, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
}
func (p *Platform) EditMessage(ctx context.Context, req cap.EditRequest) error {
	_, _, err := p.messageTarget(ctx, req.Message.Route)
	if err != nil {
		return err
	}
	chunks, err := common.SplitMessageContent(req.Content)
	if err != nil {
		return err
	}
	if len(chunks) != 1 {
		return &cap.Error{Code: cap.CodeInvalidContent, Outcome: cap.OutcomeNotSent}
	}
	// Lark's update endpoint takes only a message ID. Verify its owning chat first
	// so a forged reference cannot bypass target checks using a different chat ID.
	if _, err = p.ReadMessage(ctx, cap.ReadRequest{Message: req.Message, Caller: req.Caller, OperationID: req.OperationID}); err != nil {
		return larkUnsentError(err)
	}
	resp, callErr := p.client.Im.Message.Update(ctx, larkim.NewUpdateMessageReqBuilder().MessageId(req.Message.ID).Body(larkim.NewUpdateMessageReqBodyBuilder().MsgType("post").Content(larkMessageBody(chunks[0])).Build()).Build())
	if resp == nil {
		return &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeUnknown, Cause: callErr}
	}
	return larkDeliveryError(resp.ApiResp, resp.Code, callErr)
}

var _ cap.Messages = (*Platform)(nil)

// A failed preflight read cannot make the subsequent, unattempted write unknown.
func larkUnsentError(err error) error {
	if de, ok := err.(*cap.Error); ok {
		copy := *de
		copy.Outcome = cap.OutcomeNotSent
		return &copy
	}
	return &cap.Error{Code: cap.CodeFailed, Outcome: cap.OutcomeNotSent, Cause: err}
}
