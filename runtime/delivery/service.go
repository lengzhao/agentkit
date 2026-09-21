package delivery

import (
	"context"

	"github.com/lengzhao/agentkit"
	capsdelivery "github.com/lengzhao/agentkit/cap/delivery"
)

type standardAssistant struct{}

func (standardAssistant) ResolveRoute(ctx context.Context, input capsdelivery.RouteInput) (capsdelivery.Route, error) {
	return ResolveRoute(ctx, input)
}

func (standardAssistant) SendAssistantMessage(ctx context.Context, sender capsdelivery.Sender, parts []agentkit.ContentPart, opts capsdelivery.AssistantMessageOptions) error {
	return SendAssistantMessage(ctx, sender, parts, AssistantMessageOptions{
		Route:          opts.Route,
		Raw:            opts.Raw,
		UseContextEmit: opts.UseContextEmit,
	})
}

func (standardAssistant) SendProactiveInboxText(ctx context.Context, sender capsdelivery.Sender, text string) error {
	return SendProactiveInboxText(ctx, sender, text)
}

func NewAssistant(_ struct{}, _ struct{}) (capsdelivery.Assistant, error) {
	return standardAssistant{}, nil
}
