package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/session"
)

func (a *Runtime) runStepWithOverflowRecovery(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	retry *stepRetry,
	overflowRecoveryAttempted *bool,
) (stepOutcome, error) {
	for {
		msg, err := a.runStepWithRetry(ctx, sess, emit, retry)
		if err == nil {
			*overflowRecoveryAttempted = false
			return msg, nil
		}
		if ctx.Err() != nil {
			return msg, err
		}
		if *overflowRecoveryAttempted || !llm.IsContextOverflowError(err) || len(a.compaction) == 0 {
			return msg, err
		}
		*overflowRecoveryAttempted = true
		applied, compactErr := a.runForcedCompaction(ctx, sess)
		recoveryData := session.OverflowRecoveryData{Reason: "overflow"}
		if compactErr != nil {
			recoveryData.Error = compactErr.Error()
			_ = a.emitOverflowRecovery(ctx, sess, emit, recoveryData)
			return msg, fmt.Errorf("context overflow recovery failed: %w", compactErr)
		}
		recoveryData.Applied = applied
		if err := a.emitOverflowRecovery(ctx, sess, emit, recoveryData); err != nil {
			return msg, err
		}
		if applied == 0 {
			return msg, fmt.Errorf("context overflow recovery failed: compaction did not apply")
		}
		slog.Info("context overflow recovery compacted session, retrying step",
			"agent_id", a.id,
			"session_id", sess.ID(),
			"applied", applied,
		)
	}
}

func (a *Runtime) runForcedCompaction(ctx context.Context, sess agentkit.Session) (int, error) {
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		return 0, err
	}
	_, applied, err := compaction.ApplyAll(ctx, a.compaction, compaction.Request{
		SessionID: sess.ID(),
		AgentID:   a.id,
		Session:   sess,
		Messages:  messages,
		Force:     true,
	})
	return applied, err
}

func (a *Runtime) emitOverflowRecovery(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit, data session.OverflowRecoveryData) error {
	if err := session.AppendOverflowRecovery(ctx, sess, a.id, data); err != nil {
		return err
	}
	if emit == nil {
		return nil
	}
	return emit(ctx, agentkit.OutboundEvent{
		AgentID:   a.id,
		Type:      agentkit.EventOverflowRecovery,
		Data:      agentkit.MarshalOutboundData(data),
	})
}
