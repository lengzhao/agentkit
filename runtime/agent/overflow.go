package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/compaction"
	capsession "github.com/lengzhao/agentkit/cap/session"
	rtcompaction "github.com/lengzhao/agentkit/runtime/compaction"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session/derive"
	"github.com/lengzhao/agentkit/runtime/session/sessevents"
)

func (a *Runtime) runStepWithOverflowRecovery(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	model string,
	retry *stepRetry,
	overflowRecoveryAttempted *bool,
	pos stepPosition,
) (stepOutcome, error) {
	for {
		msg, err := a.runStepWithRetry(ctx, sess, emit, model, retry, pos)
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
		recoveryData := capsession.OverflowRecoveryData{Reason: "overflow"}
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
	before, err := latestCompactionSeq(ctx, sess)
	if err != nil {
		return 0, err
	}
	messages, err := sess.DeriveMessages(ctx)
	if err != nil {
		return 0, err
	}
	_, applied, err := rtcompaction.ApplyAll(ctx, a.compaction, compaction.Request{
		SessionID: sess.ID(),
		AgentID:   a.id,
		Session:   sess,
		Messages:  messages,
		Force:     true,
	})
	if err != nil {
		return 0, err
	}
	after, err := latestCompactionSeq(ctx, sess)
	if err != nil {
		return 0, err
	}
	if after <= before {
		// Services like prune-tool-results may report Applied without persisting a
		// compaction event. That is not real compaction: the next request would
		// carry the same oversized history, so do not retry.
		return 0, nil
	}
	return applied, nil
}

// latestCompactionSeq returns the newest session/compaction event seq, or 0.
func latestCompactionSeq(ctx context.Context, sess agentkit.Session) (agentkit.EventSeq, error) {
	events, err := derive.ReadAllEvents(ctx, sess)
	if err != nil {
		return 0, err
	}
	var seq agentkit.EventSeq
	for _, ev := range events {
		if ev.Type == agentkit.EventCompaction && ev.Seq > seq {
			seq = ev.Seq
		}
	}
	return seq, nil
}

func (a *Runtime) emitOverflowRecovery(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit, data capsession.OverflowRecoveryData) error {
	if err := sessevents.Default.AppendOverflowRecovery(ctx, sess, a.id, data); err != nil {
		return err
	}
	if emit == nil {
		return nil
	}
	return emit(ctx, agentkit.OutboundEvent{
		AgentID: a.id,
		Type:    agentkit.EventOverflowRecovery,
		Data:    rctx.MarshalOutboundData(data),
	})
}
