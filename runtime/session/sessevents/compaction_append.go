package sessevents

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	sessstore "github.com/lengzhao/agentkit/runtime/session/sessstore"
)

// AppendCompaction writes a compaction marker event and trims in-memory history
// superseded by it.
func AppendCompaction(ctx context.Context, s agentkit.Session, agentID agentkit.AgentID, data compaction.EventData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.Append(ctx, agentkit.SessionEvent{
		AgentID: agentID,
		Type:    agentkit.EventCompaction,
		Data:    raw,
	})
	if err != nil {
		return err
	}
	sessstore.TrimCompacted(s, agentID, data.MemoryCutoffSeq())
	return nil
}
