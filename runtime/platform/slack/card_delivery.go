package slack

import (
	"context"
	"errors"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	slackapi "github.com/slack-go/slack"
)

func (p *Platform) SendCard(ctx context.Context, route agentkit.RouteRef, card *cap.Card) (cap.MessageRef, error) {
	route, target, err := common.CardRoute(ctx, route, card, "slack", p.PlatformInstance())
	if err != nil {
		return cap.MessageRef{}, err
	}
	if p.client == nil {
		return cap.MessageRef{}, &cap.Error{Code: cap.CodeFailed}
	}
	if !p.channelAllowed(target.ChannelID, inferSlackChannelType(target.ChannelID)) {
		return cap.MessageRef{}, &cap.Error{Code: cap.CodeForbidden}
	}
	// The existing Slack renderer has no select control implementation.
	for _, element := range card.Elements {
		if _, ok := element.(cap.CardSelect); ok {
			return cap.MessageRef{}, &cap.Error{Code: cap.CodeUnsupported}
		}
	}
	session, _ := rctx.RouteSessionID(route)
	opts := []slackapi.MsgOption{slackapi.MsgOptionBlocks(renderCardBlocks(card, string(session))...), slackapi.MsgOptionText(card.FallbackText(), false)}
	if target.ThreadID != "" {
		opts = append(opts, slackapi.MsgOptionTS(target.ThreadID))
	}
	channel, id, err := p.client.PostMessageContext(ctx, target.ChannelID, opts...)
	if err != nil {
		return cap.MessageRef{}, cardSendError(err)
	}
	if id == "" || channel == "" {
		return cap.MessageRef{}, &cap.Error{Code: cap.CodeFailed, Unknown: true}
	}
	return cap.MessageRef{ID: id, Route: rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: "slack", Instance: route.Instance, ChannelID: channel, ThreadID: target.ThreadID})}, nil
}
func cardSendError(err error) error {
	out := &cap.Error{Code: cap.CodeTransport, Unknown: true, Cause: err}
	var rate *slackapi.RateLimitedError
	if errors.As(err, &rate) {
		out.Code = cap.CodeRateLimited
		out.Unknown = false
		return out
	}
	var api slackapi.SlackErrorResponse
	if errors.As(err, &api) {
		out.Code = cap.CodeFailed
		switch api.Err {
		case "channel_not_found", "message_not_found", "thread_not_found", "invalid_arguments":
			out.Code = cap.CodeInvalidTarget
			out.Unknown = false
		case "missing_scope", "not_in_channel", "no_permission", "not_authed", "invalid_auth", "token_revoked", "restricted_action":
			out.Code = cap.CodeForbidden
			out.Unknown = false
		case "ratelimited":
			out.Code = cap.CodeRateLimited
			out.Unknown = false
		}
	}
	return out
}

var _ cap.CardSender = (*Platform)(nil)
