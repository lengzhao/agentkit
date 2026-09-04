package agent

import (
	"context"
	"log/slog"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/session"
)

// recoverIncompleteTurn closes out a turn a previous process left open. It runs
// before every turn rather than only at startup, because the same session may be
// picked up by any process at any time, and an unanswered tool call makes the
// history unusable for the provider.
func (a *Runtime) recoverIncompleteTurn(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit) error {
	events, err := session.ReadAllEvents(ctx, sess)
	if err != nil {
		return err
	}
	incomplete := session.ScanIncomplete(events)
	if incomplete == nil {
		return nil
	}
	// A subagent turn must only touch its own KeySessionID history. If a caller
	// ever resolves the wrong session object, skip rather than repair a parent
	// delegate turn.
	if ctx.Value(agentkit.KeyInSubagent) != nil {
		turnSessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
		if turnSessionID != "" && sess.ID() != turnSessionID {
			return nil
		}
	}
	// Only repair turns owned by this agent. A subagent that accidentally reads
	// the parent session must not close out the parent's in-flight delegate.
	if incomplete.AgentID != "" && incomplete.AgentID != a.id {
		return nil
	}
	// Attribute the repair to this agent when the interrupted turn did not
	// record one, so the synthesized events are never orphaned.
	if incomplete.AgentID == "" {
		incomplete.AgentID = a.id
	}

	data, err := session.RepairIncomplete(ctx, sess, incomplete)
	if err != nil {
		return err
	}
	slog.Warn("recovered interrupted turn",
		"agent_id", a.id,
		"session_id", sess.ID(),
		"turn_start_seq", data.TurnStartSeq,
		"steps", data.Steps,
		"orphan_results", data.OrphanResults,
		"closed_step", data.ClosedStep,
	)
	if emit == nil {
		return nil
	}
	return emit(ctx, agentkit.OutboundEvent{
		SessionID: sess.ID(),
		AgentID:   a.id,
		Type:      agentkit.EventSessionRecovery,
		Data:      agentkit.MarshalOutboundData(data),
	})
}
