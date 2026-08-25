package session

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// SubagentStartData records a delegation on the *parent* session. The child's own
// work lives in its own session; these two events are the audit trail linking
// the two logs together.
type SubagentStartData struct {
	Agent   string `json:"agent"`
	Session string `json:"session"`
	Task    string `json:"task"`
}

// SubagentEndData records the outcome of a delegation. Summary duplicates what
// the model sees in the tool result, on purpose: reading the parent log alone
// should be enough to reconstruct what came back.
type SubagentEndData struct {
	Agent   string `json:"agent"`
	Session string `json:"session"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
	Steps   int    `json:"steps"`
	Error   string `json:"error,omitempty"`
}

func AppendSubagentStart(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data SubagentStartData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventSubagentStart, data)
}

func AppendSubagentEnd(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data SubagentEndData) error {
	return appendLifecycle(ctx, s, agentID, agentkit.EventSubagentEnd, data)
}
