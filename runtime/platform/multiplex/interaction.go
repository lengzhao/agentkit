package multiplex

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
)

func (m *Platform) ReadInteractionReply(ctx context.Context, req agentkit.HumanInteraction) (agentkit.InteractionReply, error) {
	platformID, _ := ctx.Value(agentkit.KeyPlatformID).(string)
	if platformID == "" {
		return agentkit.InteractionReply{}, fmt.Errorf("platform id missing, cannot route interaction")
	}
	p, ok := m.platforms[platformID]
	if !ok {
		return agentkit.InteractionReply{}, fmt.Errorf("unknown platform %q", platformID)
	}
	handler, ok := p.(agentkit.InteractionHandler)
	if !ok {
		return agentkit.InteractionReply{}, fmt.Errorf("platform %q does not support interactive reply", platformID)
	}
	return handler.ReadInteractionReply(ctx, req)
}

var _ agentkit.InteractionHandler = (*Platform)(nil)
