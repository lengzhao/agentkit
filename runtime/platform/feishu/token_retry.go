package feishu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"strings"
	"syscall"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

func (p *Platform) withFreshTenantAccessTokenRetry(ctx context.Context, operation string, fn feishuRequestFunc) error {
	err := fn(p.client)
	if !isTenantAccessTokenInvalid(err) {
		return err
	}

	freshToken, refreshErr := p.fetchFreshTenantAccessToken(ctx)
	if refreshErr != nil {
		return fmt.Errorf("%s: %s failed after token refresh attempt: %w (original error: %v)", p.tag(), operation, refreshErr, err)
	}

	slog.Warn(p.tag()+": retrying request with fresh tenant access token", "operation", operation)
	return fn(p.replayAPIClient(), larkcore.WithTenantAccessToken(freshToken))
}

func (p *Platform) fetchFreshTenantAccessToken(ctx context.Context) (string, error) {
	resp, err := p.replayAPIClient().GetTenantAccessTokenBySelfBuiltApp(ctx, &larkcore.SelfBuiltTenantAccessTokenReq{
		AppID:     p.appID,
		AppSecret: p.appSecret,
	})
	if err != nil {
		return "", fmt.Errorf("%s: fetch tenant access token: %w", p.tag(), err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("%s: fetch tenant access token code=%d msg=%s", p.tag(), resp.Code, resp.Msg)
	}
	if strings.TrimSpace(resp.TenantAccessToken) == "" {
		return "", fmt.Errorf("%s: fetch tenant access token returned empty token", p.tag())
	}
	return resp.TenantAccessToken, nil
}

func (p *Platform) replayAPIClient() *lark.Client {
	p.replayClientMu.Lock()
	defer p.replayClientMu.Unlock()
	if p.replayClient == nil {
		p.replayClient = newFeishuReplayClient(p.appID, p.appSecret, p.domain)
	}
	return p.replayClient
}

func newFeishuReplayClient(appID, appSecret, domain string) *lark.Client {
	var opts []lark.ClientOptionFunc
	opts = append(opts, lark.WithEnableTokenCache(false))
	if domain != "" && domain != lark.FeishuBaseUrl {
		opts = append(opts, lark.WithOpenBaseUrl(domain))
	}
	return lark.NewClient(appID, appSecret, opts...)
}

func isTenantAccessTokenInvalid(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "99991663") || strings.Contains(msg, "invalid access token")
}

// Transient retry constants for network-level failures.
const (
	maxTransientRetries    = 3
	transientRetryInitial  = 500 * time.Millisecond
	transientRetryMaxDelay = 5 * time.Second
)

// isTransientError returns true if the error is a transient network error
// that warrants a retry (connection reset, timeout, EOF, etc.).
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	// Typed syscall checks — more robust than string matching.
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	// net.Error covers timeouts and temporary errors from the stdlib.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// EOF usually means the server closed the connection mid-response.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// Unwrapped string checks for common transient symptoms that may
	// appear in wrapped Feishu SDK errors.
	msg := err.Error()
	for _, substr := range []string{
		"connection reset by peer",
		"broken pipe",
		"i/o timeout",
		"TLS handshake timeout",
		"server misbehaving",
		"connection refused",
	} {
		if strings.Contains(msg, substr) {
			return true
		}
	}
	return false
}

// withTransientRetry wraps an operation with exponential-backoff retry on
// transient network errors. Non-transient errors are returned immediately.
// Jitter (up to +25% of delay) is added to prevent thundering-herd retries.
func (p *Platform) withTransientRetry(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	delay := transientRetryInitial
	for attempt := 0; attempt <= maxTransientRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			if attempt > 0 {
				slog.Info(p.tag()+": transient retry succeeded",
					"operation", operation,
					"attempt", attempt+1,
				)
			}
			return nil
		}
		if !isTransientError(lastErr) {
			return lastErr
		}
		if attempt == maxTransientRetries {
			break
		}
		// Add jitter: up to +25% of delay to spread out concurrent retries.
		jitter := time.Duration(rand.Int64N(int64(delay / 4)))
		actualDelay := delay + jitter
		slog.Warn(p.tag()+": transient error, retrying",
			"operation", operation,
			"attempt", attempt+1,
			"max_retries", maxTransientRetries,
			"delay", actualDelay,
			"error", lastErr,
		)
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %s retry cancelled: %w (last error: %v)", p.tag(), operation, ctx.Err(), lastErr)
		case <-time.After(actualDelay):
		}
		delay = min(delay*2, transientRetryMaxDelay)
	}
	return fmt.Errorf("%s failed after %d retries: %w", operation, maxTransientRetries, lastErr)
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
