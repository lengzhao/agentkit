package sessevents

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
)

// AppendCompaction writes a compaction marker event. Compaction events are
// self-describing: session backends trim their own in-memory history when
// appending one, and derive.DeriveMessages always honors the latest marker.
func (events) AppendCompaction(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data compaction.EventData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.Append(ctx, agentkit.SessionEvent{
		AgentID: agentID,
		Type:    agentkit.EventCompaction,
		Data:    raw,
	})
	return err
}
