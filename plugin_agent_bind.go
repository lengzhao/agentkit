package agentkit

import "context"

// AgentBindStore reads and writes per-session agent routing beside session logs.
// Missing bind means Runner falls back to loop.defaultAgent.
type AgentBindStore interface {
	AgentBind(ctx context.Context, id SessionID) (AgentID, error)
	SetAgentBind(ctx context.Context, id SessionID, agent AgentID) error
}
