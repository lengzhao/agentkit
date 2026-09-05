package compaction

import (
	"context"
	"errors"
	"time"

	capscompaction "github.com/lengzhao/agentkit/cap/compaction"
)

// ResolveRetrySettings applies defaults aligned with agent-level auto retry.
func ResolveRetrySettings(cfg *capscompaction.RetryConfig) capscompaction.RetryPolicy {
	out := capscompaction.RetryPolicy{
		Enabled:     true,
		MaxRetries:  3,
		BaseDelayMs: 2000,
	}
	if cfg == nil {
		return out
	}
	if cfg.Enabled != nil {
		out.Enabled = *cfg.Enabled
	}
	if cfg.MaxRetries > 0 {
		out.MaxRetries = cfg.MaxRetries
	}
	if cfg.BaseDelayMs > 0 {
		out.BaseDelayMs = cfg.BaseDelayMs
	}
	return out
}

// RetryCall runs call with bounded retries. isRetryable classifies transient failures;
// when nil, only context cancellation is treated as non-retryable.
func RetryCall(
	ctx context.Context,
	policy capscompaction.RetryPolicy,
	isRetryable func(error) bool,
	call func() error,
	callbacks *capscompaction.SummarizationRetryCallbacks,
) error {
	if !policy.Enabled || policy.MaxRetries <= 0 {
		return call()
	}
	if isRetryable == nil {
		isRetryable = func(err error) bool { return err != nil }
	}

	var lastRetryAttempt int
	for attempt := 0; ; attempt++ {
		err := call()
		if err == nil {
			if lastRetryAttempt > 0 {
				invokeFinished(callbacks, true, lastRetryAttempt, "")
			}
			return nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if lastRetryAttempt > 0 {
				invokeFinished(callbacks, false, lastRetryAttempt, err.Error())
			}
			return err
		}
		if attempt >= policy.MaxRetries || !isRetryable(err) {
			if lastRetryAttempt > 0 {
				invokeFinished(callbacks, false, lastRetryAttempt, err.Error())
			}
			return err
		}

		retryAttempt := attempt + 1
		lastRetryAttempt = retryAttempt
		delayMs := policy.BaseDelayMs << attempt
		invokeScheduled(callbacks, retryAttempt, policy.MaxRetries, delayMs, err.Error())
		if err := sleepContext(ctx, time.Duration(delayMs)*time.Millisecond); err != nil {
			invokeFinished(callbacks, false, retryAttempt, "retry cancelled")
			return err
		}
	}
}

func invokeScheduled(cb *capscompaction.SummarizationRetryCallbacks, attempt, maxAttempts, delayMs int, errorMessage string) {
	if cb != nil && cb.OnScheduled != nil {
		cb.OnScheduled(attempt, maxAttempts, delayMs, errorMessage)
	}
}

func invokeFinished(cb *capscompaction.SummarizationRetryCallbacks, success bool, attempt int, finalError string) {
	if cb != nil && cb.OnFinished != nil {
		cb.OnFinished(success, attempt, finalError)
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
