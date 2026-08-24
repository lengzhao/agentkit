package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/llm"
	"github.com/lengzhao/agentkit/runtime/session"
)

type RetryConfig struct {
	Enabled     *bool `json:"enabled"`
	MaxRetries  int   `json:"maxRetries"`
	BaseDelayMs int   `json:"baseDelayMs"`
}

type retrySettings struct {
	enabled     bool
	maxRetries  int
	baseDelayMs int
}

func resolveRetrySettings(cfg *RetryConfig) retrySettings {
	out := retrySettings{
		enabled:     true,
		maxRetries:  3,
		baseDelayMs: 2000,
	}
	if cfg == nil {
		return out
	}
	if cfg.Enabled != nil {
		out.enabled = *cfg.Enabled
	}
	if cfg.MaxRetries > 0 {
		out.maxRetries = cfg.MaxRetries
	}
	if cfg.BaseDelayMs > 0 {
		out.baseDelayMs = cfg.BaseDelayMs
	}
	return out
}

type stepRetry struct {
	settings retrySettings
	attempt  int
}

func newStepRetry(settings retrySettings) *stepRetry {
	return &stepRetry{settings: settings}
}

func (r *stepRetry) delayMs() int {
	if r.attempt <= 0 {
		return 0
	}
	return r.settings.baseDelayMs << (r.attempt - 1)
}

func (r *stepRetry) shouldRetry(err error) bool {
	if err == nil || !r.settings.enabled {
		return false
	}
	if r.attempt >= r.settings.maxRetries {
		return false
	}
	return llm.IsRetryableError(err)
}

func (r *stepRetry) begin(err error) (session.AutoRetryStartData, bool) {
	if !r.shouldRetry(err) {
		return session.AutoRetryStartData{}, false
	}
	r.attempt++
	data := session.AutoRetryStartData{
		Attempt:      r.attempt,
		MaxAttempts:  r.settings.maxRetries,
		DelayMs:      r.delayMs(),
		ErrorMessage: err.Error(),
	}
	return data, true
}

func (r *stepRetry) reset() {
	r.attempt = 0
}

func (a *Runtime) runStepWithRetry(
	ctx context.Context,
	sess agentkit.Session,
	emit agentkit.OutboundEmit,
	retry *stepRetry,
) (stepOutcome, error) {
	for {
		msg, err := a.runStep(ctx, sess, emit)
		if err == nil {
			if retry.attempt > 0 {
				_ = a.emitAutoRetryEnd(ctx, sess, emit, session.AutoRetryEndData{
					Success: true,
					Attempt: retry.attempt,
				})
			}
			retry.reset()
			return msg, nil
		}
		if ctx.Err() != nil {
			return msg, err
		}
		start, ok := retry.begin(err)
		if !ok {
			if retry.attempt > 0 {
				_ = a.emitAutoRetryEnd(ctx, sess, emit, session.AutoRetryEndData{
					Success:    false,
					Attempt:    retry.attempt,
					FinalError: err.Error(),
				})
			}
			retry.reset()
			return msg, err
		}
		if err := a.emitAutoRetryStart(ctx, sess, emit, start); err != nil {
			return msg, err
		}
		if err := llm.SleepContext(ctx, time.Duration(start.DelayMs)*time.Millisecond); err != nil {
			_ = a.emitAutoRetryEnd(ctx, sess, emit, session.AutoRetryEndData{
				Success:    false,
				Attempt:    retry.attempt,
				FinalError: "retry cancelled",
			})
			retry.reset()
			return msg, err
		}
	}
}

func (a *Runtime) emitAutoRetryStart(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit, data session.AutoRetryStartData) error {
	slog.Warn("llm auto retry scheduled",
		"agent_id", a.id,
		"session_id", sess.ID(),
		"attempt", data.Attempt,
		"max_attempts", data.MaxAttempts,
		"delay_ms", data.DelayMs,
		"error", data.ErrorMessage,
	)
	if err := session.AppendAutoRetryStart(ctx, sess, a.id, data); err != nil {
		return err
	}
	if emit == nil {
		return nil
	}
	return emit(ctx, agentkit.OutboundEvent{
		SessionID: sess.ID(),
		AgentID:   a.id,
		Type:      agentkit.EventAutoRetryStart,
		Data:      agentkit.MarshalOutboundData(data),
	})
}

func (a *Runtime) emitAutoRetryEnd(ctx context.Context, sess agentkit.Session, emit agentkit.OutboundEmit, data session.AutoRetryEndData) error {
	slog.Info("llm auto retry finished",
		"agent_id", a.id,
		"session_id", sess.ID(),
		"attempt", data.Attempt,
		"success", data.Success,
		"final_error", data.FinalError,
	)
	if err := session.AppendAutoRetryEnd(ctx, sess, a.id, data); err != nil {
		return err
	}
	if emit == nil {
		return nil
	}
	return emit(ctx, agentkit.OutboundEvent{
		SessionID: sess.ID(),
		AgentID:   a.id,
		Type:      agentkit.EventAutoRetryEnd,
		Data:      agentkit.MarshalOutboundData(data),
	})
}
