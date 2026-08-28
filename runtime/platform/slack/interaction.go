package slack

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	"github.com/lengzhao/agentkit/runtime/platform/common"
)

func (p *Platform) sendPermissionCard(ctx context.Context, event agentkit.OutboundEvent) error {
	var payload permission.RequestPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return err
	}
	raw, ok := p.deliveries.Load(event.SessionID)
	if !ok {
		return nil
	}
	d := raw.(delivery)
	card := common.PermissionCardFromPayload(payload)
	return p.postCard(ctx, d, card)
}
