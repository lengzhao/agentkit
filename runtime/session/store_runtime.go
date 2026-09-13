package session

import (
	"context"

	"github.com/lengzhao/agentkit"
)

func (s *Store) AgentBind(ctx context.Context, id agentkit.SessionID) (agentkit.AgentID, error) {
	return s.sidecar.AgentBind(ctx, id)
}

func (s *Store) SetAgentBind(ctx context.Context, id agentkit.SessionID, agent agentkit.AgentID) error {
	return s.sidecar.SetAgentBind(ctx, id, agent)
}

func (s *Store) ModelBind(ctx context.Context, id agentkit.SessionID) (string, error) {
	return s.sidecar.ModelBind(ctx, id)
}

func (s *Store) SetModelBind(ctx context.Context, id agentkit.SessionID, model string) error {
	return s.sidecar.SetModelBind(ctx, id, model)
}

func (s *Store) ActiveSession(ctx context.Context, id agentkit.SessionID) (agentkit.SessionID, error) {
	return s.sidecar.ActiveSession(ctx, id)
}

func (s *Store) SetActiveSession(ctx context.Context, id, active agentkit.SessionID) error {
	return s.sidecar.SetActiveSession(ctx, id, active)
}

func (s *Store) storeDir(ctx context.Context) (string, error) {
	return ensureTenantLayout(ctx, s.workspace, s.relDir)
}
