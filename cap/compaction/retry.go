package compaction

import (
	"context"
	"errors"
	"time"
)

// RetryConfig controls bounded retries with exponential backoff for compaction LLM calls.
type RetryConfig struct {
	Enabled     *bool `json:"enabled"`
	MaxRetries  int   `json:"maxRetries"`
	BaseDelayMs int   `json:"baseDelayMs"`
}

// RetryPolicy is the resolved retry configuration.
type RetryPolicy struct {
	Enabled     bool
	MaxRetries  int
	BaseDelayMs int
}

// SummarizationRetryCallbacks are optional hooks for compaction summarization retries.
type SummarizationRetryCallbacks struct {
	OnScheduled func(attempt, maxAttempts, delayMs int, errorMessage string)
	OnFinished  func(success bool, attempt int, finalError string)
}

// ResolveRetrySettings applies defaults aligned with agent-level auto retry.
func ResolveRetrySettings(cfg *RetryConfig) RetryPolicy {
	out := RetryPolicy{
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
	policy RetryPolicy,
	isRetryable func(error) bool,
	call func() error,
	callbacks *SummarizationRetryCallbacks,
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

func invokeScheduled(cb *SummarizationRetryCallbacks, attempt, maxAttempts, delayMs int, errorMessage string) {
	if cb != nil && cb.OnScheduled != nil {
		cb.OnScheduled(attempt, maxAttempts, delayMs, errorMessage)
	}
}

func invokeFinished(cb *SummarizationRetryCallbacks, success bool, attempt int, finalError string) {
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
