package slack

import (
	"context"
	"errors"
	"strings"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	slackapi "github.com/slack-go/slack"
)

func (p *Platform) messageTarget(ctx context.Context, route agentkit.RouteRef) (agentkit.RouteRef, agentkit.SessionRouteTarget, error) {
	route, target, err := common.MessageRoute(ctx, route, "slack", p.PlatformInstance())
	if err != nil {
		return route, target, err
	}
	if p.client == nil {
		return route, target, &cap.Error{Code: cap.CodeFailed, Outcome: cap.OutcomeNotSent}
	}
	if !p.channelAllowed(target.ChannelID, inferSlackChannelType(target.ChannelID)) {
		return route, target, &cap.Error{Code: cap.CodeForbidden, Outcome: cap.OutcomeNotSent}
	}
	return route, target, nil
}
func slackDeliveryError(err error) error {
	if err == nil {
		return nil
	}
	out := &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeUnknown, Cause: err}
	var rate *slackapi.RateLimitedError
	if errors.As(err, &rate) {
		out.Code = cap.CodeRateLimited
		out.Outcome = cap.OutcomeNotSent
		out.RetryAfter = rate.RetryAfter
		return out
	}
	var api slackapi.SlackErrorResponse
	if errors.As(err, &api) {
		out.Code = cap.CodeFailed
		switch api.Err {
		case "channel_not_found", "message_not_found", "thread_not_found", "invalid_arguments", "invalid_ts_latest", "invalid_ts_oldest":
			out.Code = cap.CodeInvalidTarget
			out.Outcome = cap.OutcomeNotSent
		case "missing_scope", "not_in_channel", "no_permission", "cant_update_message", "not_authed", "invalid_auth", "token_revoked", "account_inactive", "restricted_action":
			out.Code = cap.CodeForbidden
			out.Outcome = cap.OutcomeNotSent
		case "ratelimited":
			out.Code = cap.CodeRateLimited
			out.Outcome = cap.OutcomeNotSent
			// Internal errors can occur after committing a write; leave unknown.
		}
	}
	return out
}
func slackMessageText(parts []cap.Part) string {
	var b strings.Builder
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	for _, part := range parts {
		if part.Type == "mention" {
			b.WriteString("<@" + part.UserID + ">")
		} else {
			b.WriteString(escape.Replace(part.Text))
		}
	}
	return b.String()
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
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return common.FailMessagePart(receipt, i, &cap.Error{Code: cap.CodeTransport, Outcome: cap.OutcomeNotSent, Cause: err})
		}
		opts := []slackapi.MsgOption{slackapi.MsgOptionText(slackMessageText(chunk), false), slackapi.MsgOptionParse(false), slackapi.MsgOptionLinkNames(false), slackapi.MsgOptionDisableLinkUnfurl(), slackapi.MsgOptionDisableMediaUnfurl()}
		if target.ThreadID != "" {
			opts = append(opts, slackapi.MsgOptionTS(target.ThreadID))
		}
		channel, id, err := p.client.PostMessageContext(ctx, target.ChannelID, opts...)
		if err != nil {
			return common.FailMessagePart(receipt, i, slackDeliveryError(err))
		}
		if id == "" || channel == "" {
			return common.FailMessagePart(receipt, i, &cap.Error{Code: cap.CodeFailed, Outcome: cap.OutcomeUnknown})
		}
		actual := rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "slack", Instance: route.Instance, ChannelID: channel, ThreadID: target.ThreadID})
		receipt.Messages = append(receipt.Messages, cap.MessageRef{Route: actual, ID: id})
		if topic && i == 0 {
			target.ThreadID = id
			receipt.Route = rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "slack", Instance: route.Instance, ChannelID: channel, ThreadID: id})
		} else if !topic {
			receipt.Route = actual
		}
	}
	receipt.Outcome = cap.OutcomeConfirmed
	// Slack has no documented bounded deduplication guarantee for this API. No
	// local cache or unsafe automatic retry is used, even with an operation ID.
	return receipt, nil
}
func (p *Platform) EditMessage(ctx context.Context, req cap.EditRequest) error {
	_, target, err := p.messageTarget(ctx, req.Message.Route)
	if err != nil {
		return err
	}
	if req.Message.ID == "" {
		return &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
	}
	chunks, err := common.SplitMessageContent(req.Content)
	if err != nil {
		return err
	}
	if len(chunks) != 1 {
		return &cap.Error{Code: cap.CodeInvalidContent, Outcome: cap.OutcomeNotSent}
	}
	_, _, _, err = p.client.UpdateMessageContext(ctx, target.ChannelID, req.Message.ID, slackapi.MsgOptionText(slackMessageText(chunks[0]), false), slackapi.MsgOptionParse(false))
	return slackDeliveryError(err)
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
	var items []slackapi.Message
	if target.ThreadID != "" {
		items, _, _, err = p.client.GetConversationRepliesContext(ctx, &slackapi.GetConversationRepliesParameters{ChannelID: target.ChannelID, Timestamp: target.ThreadID, Oldest: ref.ID, Latest: ref.ID, Inclusive: true, Limit: 1})
	} else {
		var resp *slackapi.GetConversationHistoryResponse
		resp, err = p.client.GetConversationHistoryContext(ctx, &slackapi.GetConversationHistoryParameters{ChannelID: target.ChannelID, Oldest: ref.ID, Latest: ref.ID, Inclusive: true, Limit: 1})
		if resp != nil {
			items = resp.Messages
		}
	}
	if err != nil {
		return cap.Message{}, slackDeliveryError(err)
	}
	for _, item := range items {
		if item.Timestamp == ref.ID && !item.Hidden {
			return cap.Message{Ref: ref, Content: []cap.Part{{Type: "text", Text: extractSlackBlockMessageText(item)}}}, nil
		}
	}
	return cap.Message{}, &cap.Error{Code: cap.CodeInvalidTarget, Outcome: cap.OutcomeNotSent}
}

var _ cap.Messages = (*Platform)(nil)
