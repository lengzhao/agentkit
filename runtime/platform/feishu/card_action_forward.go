package feishu

import (
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	"github.com/lengzhao/agentkit/cap/interaction"
)

// forwardUnknownCardAction receives the single snapshot made before ACK.
func (p *Platform) forwardUnknownCardAction(e interaction.Event, complete func(error)) (*callback.CardActionTriggerResponse, error) {
	go func() { complete(p.processCardInteraction(e)) }()
	return cardActionAckToast(), nil
}
func cardActionAckToast() *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "info", Content: "已提交"}}
}
