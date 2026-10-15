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

// safetyStopError marks a step that kept failing with a provider content-safety
// rejection after the compact-and-retry recovery. runSegment converts it into a
// graceful turn end (synthesized explanation delivered like a normal reply)
// instead of a failed turn with no user-visible output.
type safetyStopError struct{ err error }

func (e *safetyStopError) Error() string { return e.err.Error() }
func (e *safetyStopError) Unwrap() error { return e.err }

func (a *Runtime) runStepWithOverflowRecovery(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	model string,
	retry *stepRetry,
	overflowRecoveryAttempted *bool,
	safetyRecoveryAttempted *bool,
	pos stepPosition,
) (stepOutcome, error) {
	for {
		msg, err := a.runStepWithRetry(ctx, sess, emit, model, retry, pos)
		if err == nil {
			*overflowRecoveryAttempted = false
			*safetyRecoveryAttempted = false
			return msg, nil
		}
		if ctx.Err() != nil {
			return msg, err
		}
		if llm.IsSafetyError(err) {
			// Content-safety rejections are deterministic for identical input.
			// The offending content is almost always something in the history
			// (a fetched page, an email, a tool dump), so one compact-and-retry
			// replaces it with a bounded summary; if that still fails, degrade
			// gracefully instead of killing the whole turn mid-flight.
			if !*safetyRecoveryAttempted && len(a.compaction) > 0 {
				*safetyRecoveryAttempted = true
				applied, compactErr := a.runForcedCompaction(ctx, sess)
				recoveryData := capsession.OverflowRecoveryData{Reason: "safety"}
				if compactErr != nil {
					recoveryData.Error = compactErr.Error()
					_ = a.emitOverflowRecovery(ctx, sess, emit, recoveryData)
					return msg, &safetyStopError{err: err}
				}
				recoveryData.Applied = applied
				if emitErr := a.emitOverflowRecovery(ctx, sess, emit, recoveryData); emitErr != nil {
					return msg, emitErr
				}
				if applied > 0 {
					slog.Info("content-safety rejection: compacted session, retrying step",
						"agent_id", a.id,
						"session_id", sess.ID(),
						"applied", applied,
					)
					continue
				}
			}
			return msg, &safetyStopError{err: err}
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

// safetyBlockedNotice is the synthesized final reply for a turn the provider's
// content-safety system refused even after compact-and-retry. It is persisted
// and delivered like a normal assistant message so the user never faces a
// silent, failed turn (previously: 57 steps of work ended with no output).
const safetyBlockedNotice = "本轮请求被模型服务商的内容安全系统拦截（通常由某条工具返回或历史内容触发），自动压缩上下文后重试仍被拒绝，本轮只能到此为止。\n\n建议：换一种表述重发，或让我换个角度/缩小范围继续；若反复出现请联系平台管理员。\n\n(The provider's content-safety system blocked this request; context compaction and retry did not help, so the turn ends here.)"

// finishSafetyBlockedTurn persists and delivers a graceful final message for a
// safety-blocked step, ending the turn normally with StopSafetyBlocked instead
// of propagating a hard error that leaves the user with no output at all.
func (a *Runtime) finishSafetyBlockedTurn(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	run *turnRun,
	sse *safetyStopError,
) (agentkit.TurnStopReason, error) {
	slog.Warn("content-safety rejection persisted after recovery, ending turn gracefully",
		"agent_id", a.id,
		"session_id", sess.ID(),
		"err", sse.err,
	)
	msg := agentkit.ModelMessage{
		Role: "assistant",
		Content: []agentkit.ContentPart{{
			Type: "text",
			Text: safetyBlockedNotice,
		}},
	}
	// Persist with WithoutCancel so a half-cancelled step context cannot lose
	// the explanation from the durable history.
	if err := appendAssistantMessage(context.WithoutCancel(ctx), sess, a.id, msg); err != nil {
		return "", err
	}
	if err := newStreamEmitter(ctx, sess.ID(), a.id, emit).finalize(msg); err != nil {
		return "", err
	}
	final := msg
	run.finalMessage = &final
	return agentkit.StopSafetyBlocked, nil
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
