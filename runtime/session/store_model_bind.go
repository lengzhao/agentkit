package session

import (
	"context"

	"github.com/lengzhao/agentkit"
)

func (s *Store) ModelBind(ctx context.Context, id agentkit.SessionID) (string, error) {
	return s.sidecar.ModelBind(ctx, id)
}

func (s *Store) SetModelBind(ctx context.Context, id agentkit.SessionID, model string) error {
	return s.sidecar.SetModelBind(ctx, id, model)
}
