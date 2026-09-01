package llm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/lengzhao/agentkit"
	openai "github.com/sashabaranov/go-openai"
)

type ProviderRetrySettings struct {
	MaxRetries      int `json:"maxRetries"`
	MaxRetryDelayMs int `json:"maxRetryDelayMs"`
}

type LLMRetryConfig struct {
	Provider *ProviderRetrySettings `json:"provider,omitempty"`
}

func defaultProviderRetry(cfg *ProviderRetrySettings) ProviderRetrySettings {
	out := ProviderRetrySettings{MaxRetryDelayMs: 60_000}
	if cfg == nil {
		return out
	}
	out.MaxRetries = cfg.MaxRetries
	if cfg.MaxRetryDelayMs > 0 {
		out.MaxRetryDelayMs = cfg.MaxRetryDelayMs
	} else if cfg.MaxRetryDelayMs == 0 && cfg.MaxRetries > 0 {
		out.MaxRetryDelayMs = 0
	}
	return out
}

var (
	nonRetryableQuotaPattern = regexp.MustCompile(`(?i)` + strings.Join([]string{
		`GoUsageLimitError`,
		`FreeUsageLimitError`,
		`Monthly usage limit reached`,
		`available balance`,
		`insufficient_quota`,
		`out of budget`,
		`quota exceeded`,
		`billing`,
	}, "|"))
	retryableErrorPattern = regexp.MustCompile(`(?i)` + strings.Join([]string{
		`overloaded`,
		`rate.?limit`,
		`too many requests`,
		`\b429\b`,
		`\b500\b`,
		`\b502\b`,
		`\b503\b`,
		`\b504\b`,
		`\b524\b`,
		`service.?unavailable`,
		`server.?error`,
		`internal.?error`,
		`provider.?returned.?error`,
		`exceeded request buffer limit while retrying upstream`,
		`network.?error`,
		`connection.?error`,
		`connection.?refused`,
		`connection.?lost`,
		`other side closed`,
		`fetch failed`,
		`getaddrinfo`,
		`ENOTFOUND`,
		`EAI_AGAIN`,
		`upstream.?connect`,
		`reset before headers`,
		`socket hang up`,
		`socket connection was closed`,
		`timed? out`,
		`timeout`,
		`terminated`,
		`websocket.?closed`,
		`websocket.?error`,
		`ended without`,
		`stream ended before message_stop`,
		`stream ended before a terminal response event`,
		`http2 request did not get a response`,
		`retry delay`,
		`you can retry your request`,
		`try your request again`,
		`please retry your request`,
		`ResourceExhausted`,
	}, "|"))
	contextOverflowPattern = regexp.MustCompile(`(?i)` + strings.Join([]string{
		`context.?length`,
		`maximum context`,
		`context.?window`,
		`too many tokens`,
		`prompt is too long`,
		`input is too long`,
	}, "|"))
)

// IsRetryableError reports whether an error looks transient and safe to retry.
// Context overflow and quota exhaustion are excluded.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if IsContextOverflowError(err) {
		return false
	}
	msg := err.Error()
	if nonRetryableQuotaPattern.MatchString(msg) {
		return false
	}
	if isRetryableHTTPStatus(HTTPStatusFromError(err)) {
		return !nonRetryableQuotaPattern.MatchString(msg)
	}
	return retryableErrorPattern.MatchString(msg)
}

// IsContextOverflowError reports context-length failures that should use compaction instead of retry.
func IsContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	return contextOverflowPattern.MatchString(err.Error())
}

// IsQuotaError reports quota or billing failures that are not worth retrying on the same provider.
func IsQuotaError(err error) bool {
	if err == nil {
		return false
	}
	return nonRetryableQuotaPattern.MatchString(err.Error())
}

func HTTPStatusFromError(err error) int {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatusCode > 0 {
		return apiErr.HTTPStatusCode
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) && reqErr.HTTPStatusCode > 0 {
		return reqErr.HTTPStatusCode
	}
	return 0
}

func isRetryableHTTPStatus(status int) bool {
	if status == 0 {
		return false
	}
	return status == http.StatusRequestTimeout ||
		status == http.StatusConflict ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

func streamWithProviderRetry(
	ctx context.Context,
	cfg ProviderRetrySettings,
	open func() (agentkit.LLMStream, error),
) (agentkit.LLMStream, error) {
	retriesRemaining := cfg.MaxRetries
	retryIndex := 0
	for {
		stream, err := open()
		if err == nil {
			return stream, nil
		}
		if retriesRemaining <= 0 || !IsRetryableError(err) {
			return nil, err
		}
		delay, delayErr := providerRetryDelay(retryIndex, cfg.MaxRetryDelayMs)
		if delayErr != nil {
			return nil, fmt.Errorf("%w: %v", err, delayErr)
		}
		if err := sleepContext(ctx, delay); err != nil {
			return nil, err
		}
		retriesRemaining--
		retryIndex++
	}
}

func providerRetryDelay(retryIndex int, maxRetryDelayMs int) (time.Duration, error) {
	seconds := math.Min(0.5*math.Pow(2, float64(retryIndex)), 8)
	delay := time.Duration(seconds * float64(time.Second))
	delay += time.Duration(rand.Float64() * float64(delay) * 0.25)
	if maxRetryDelayMs > 0 && delay > time.Duration(maxRetryDelayMs)*time.Millisecond {
		return 0, fmt.Errorf("retry delay %s exceeds max %dms", delay, maxRetryDelayMs)
	}
	return delay, nil
}

func SleepContext(ctx context.Context, delay time.Duration) error {
	return sleepContext(ctx, delay)
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
