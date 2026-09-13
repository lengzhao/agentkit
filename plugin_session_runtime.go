package agentkit

import "context"

// SessionRuntimeStore reads and writes per-session agent and model overrides
// beside session logs (runtime.json). Missing fields fall back to global overlay
// and agent defaults.
type SessionRuntimeStore interface {
	AgentBind(ctx context.Context, id SessionID) (AgentID, error)
	SetAgentBind(ctx context.Context, id SessionID, agent AgentID) error
	ModelBind(ctx context.Context, id SessionID) (string, error)
	SetModelBind(ctx context.Context, id SessionID, model string) error
}
