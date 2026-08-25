package multiplex

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/interaction"
)

func (m *Platform) ReadReply(ctx context.Context, req interaction.Human) (interaction.Reply, error) {
	platformID, _ := ctx.Value(agentkit.KeyPlatformID).(string)
	if platformID == "" {
		return interaction.Reply{}, fmt.Errorf("platform id missing, cannot route interaction")
	}
	p, ok := m.platforms[platformID]
	if !ok {
		return interaction.Reply{}, fmt.Errorf("unknown platform %q", platformID)
	}
	handler, ok := p.(interaction.Handler)
	if !ok {
		return interaction.Reply{}, fmt.Errorf("platform %q does not support interactive reply", platformID)
	}
	return handler.ReadReply(ctx, req)
}

var _ interaction.Handler = (*Platform)(nil)
