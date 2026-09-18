package delivery

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

// AssistantMessageOptions configures proactive assistant outbound delivery.
type AssistantMessageOptions struct {
	Route capsdelivery.RouteInput
	// Raw skips platform markdown conversion when the transport supports it.
	Raw bool
	// UseContextEmit delivers through the per-turn OutboundEmit hook when the route
	// is the current inbox (empty SessionID and UserID).
	UseContextEmit bool
}

// SendAssistantMessage delivers a user-visible assistant message on the resolved route.
func SendAssistantMessage(ctx context.Context, sender capsdelivery.Sender, parts []agentkit.ContentPart, opts AssistantMessageOptions) error {
	if len(parts) == 0 {
		return fmt.Errorf("assistant message requires content")
	}
	route, err := ResolveRoute(ctx, opts.Route)
	if err != nil {
		return err
	}
	modelMsg := agentkit.ModelMessage{Role: "assistant", Content: parts}
	event := agentkit.OutboundEvent{
		Route:      OutboundRoute(route.PlatformID, route.SessionID),
		AgentID:    route.AgentID,
		PlatformID: route.PlatformID,
		UserID:     route.UserID,
		Type:       agentkit.EventAssistantMessage,
		Data:       rctx.MarshalOutboundData(modelMsg),
	}
	if err := event.RequirePlatformID(); err != nil {
		return err
	}
	if opts.Raw {
		ctx = context.WithValue(ctx, agentkit.KeyProactiveSendRaw, true)
	}
	if opts.UseContextEmit && routeIsCurrentInbox(opts.Route) {
		if emit := rctx.OutboundEmitFromContext(ctx); emit != nil {
			ctx = context.WithValue(ctx, agentkit.KeyProactiveSendUsed, true)
			return emit(ctx, event)
		}
	}
	if sender == nil {
		return fmt.Errorf("delivery sender is required")
	}
	return sender.Send(ctx, event)
}

// SendAssistantText delivers a single text assistant line (no-op when text is empty).
func SendAssistantText(ctx context.Context, sender capsdelivery.Sender, text string, opts AssistantMessageOptions) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return SendAssistantMessage(ctx, sender, []agentkit.ContentPart{{Type: "text", Text: text}}, opts)
}

func routeIsCurrentInbox(route capsdelivery.RouteInput) bool {
	return strings.TrimSpace(route.SessionID) == "" && strings.TrimSpace(route.UserID) == ""
}

// SendProactiveInboxText delivers raw assistant text on the current inbox route (emit when wired).
func SendProactiveInboxText(ctx context.Context, sender capsdelivery.Sender, text string) error {
	return SendAssistantText(ctx, sender, text, AssistantMessageOptions{
		Raw:            true,
		UseContextEmit: true,
	})
}
