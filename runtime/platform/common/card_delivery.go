package common

import (
	"context"
	"strings"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// CardRoute resolves a background target without per-turn state or caches.
func CardRoute(ctx context.Context, route agentkit.RouteRef, card *Card, protocol, instance string) (agentkit.RouteRef, agentkit.SessionRouteTarget, error) {
	if err := ctx.Err(); err != nil {
		return route, agentkit.SessionRouteTarget{}, &cap.Error{Code: cap.CodeTransport, Cause: err}
	}
	if card == nil || (len(card.Elements) == 0 && (card.Header == nil || strings.TrimSpace(card.Header.Title) == "")) {
		return route, agentkit.SessionRouteTarget{}, &cap.Error{Code: cap.CodeInvalidCard}
	}
	if instance == "" {
		instance = protocol
	}
	if route.DestinationID() != instance || (route.Platform != "" && route.Platform != protocol) {
		return route, agentkit.SessionRouteTarget{}, &cap.Error{Code: cap.CodeInvalidTarget}
	}
	target, ok := rctx.DecodeSessionRoute(route)
	if !ok || target.ChannelID == "" {
		return route, target, &cap.Error{Code: cap.CodeInvalidTarget}
	}
	route = rctx.BuildSessionRoute(agentkit.SessionRouteInput{Platform: protocol, Instance: instance, ChannelID: target.ChannelID, ThreadID: target.ThreadID})
	return route, target, nil
}
