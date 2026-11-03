package multiplex

import (
	"context"

	"github.com/lengzhao/agentkit"
	cap "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// SendCard forwards an optional capability using the same instance routing as Send.
func (m *Platform) SendCard(ctx context.Context, route agentkit.RouteRef, card *cap.Card) (cap.MessageRef, error) {
	destination := route.DestinationID()
	p, ok := m.platforms[destination]
	if !ok {
		return cap.MessageRef{}, &cap.Error{Code: cap.CodeInvalidTarget}
	}
	sender, ok := p.(cap.CardSender)
	if !ok {
		return cap.MessageRef{}, &cap.Error{Code: cap.CodeUnsupported}
	}
	route, err := rctx.RouteForPlatform(route, p.(agentkit.PlatformIdentifier).PlatformID())
	if err != nil {
		return cap.MessageRef{}, &cap.Error{Code: cap.CodeInvalidTarget, Cause: err}
	}
	route.Instance = destination
	return sender.SendCard(ctx, route, card)
}
