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
	transientRetryInitial = 500 * time.Millisecond
)

// transientRetryProfile 描述一组 transient 重试预算。整体策略：各档都保持
// 短预算、快速失败/快速降级——长故障窗口下尽快走完降级链（新最终卡 → 纯文本），
// 最终投递保证由降级链末端纯文本的 5s/20s/60s 长重试覆盖，而不是让卡片路径
// 在故障期长时间重试锤网关。
type transientRetryProfile struct {
	maxRetries int
	maxDelay   time.Duration
}

var (
	// retryProfileStandard：普通发送/消息类请求，2 次重试、~1.5s（0.5+1）。
	retryProfileStandard = transientRetryProfile{maxRetries: 2, maxDelay: 5 * time.Second}
	// retryProfileQuick：中途流式帧（元素流式/整卡 patch），1 次重试、~1s
	// 快速失败——内容在 streamState 单调累积，下一次 flush 自动补齐。
	retryProfileQuick = transientRetryProfile{maxRetries: 1, maxDelay: 1 * time.Second}
	// retryProfileFinalize：turn/end 定稿 patch，3 次重试、~4s——只覆盖短暂
	// 抖动；窗口更长时快速进入降级链（另发最终卡 + 撤回旧卡 → 纯文本兜底）。
	retryProfileFinalize = transientRetryProfile{maxRetries: 3, maxDelay: 5 * time.Second}
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
	// 出口代理/边缘网关（TLB 等）拦截响应：非 JSON 空 body + 403/5xx。
	// Lark API 自身永远返回 JSON，非 JSON 响应说明请求未到达应用层，
	// 属于网关抖动，退避重试通常成功（CardKit 同 seq 重试已被 300317
	// 良性化兜底，重复落地无副作用）。
	if strings.Contains(msg, "response content-type not json") {
		return true
	}
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
// transient network errors (standard profile). Non-transient errors are
// returned immediately.
func (p *Platform) withTransientRetry(ctx context.Context, operation string, fn func() error) error {
	return p.withTransientRetryProfile(ctx, operation, retryProfileStandard, fn)
}

// withTransientRetryProfile 按给定预算做指数退避重试。
// Jitter (up to +25% of delay) is added to prevent thundering-herd retries.
func (p *Platform) withTransientRetryProfile(ctx context.Context, operation string, profile transientRetryProfile, fn func() error) error {
	var lastErr error
	delay := transientRetryInitial
	for attempt := 0; attempt <= profile.maxRetries; attempt++ {
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
		if attempt == profile.maxRetries {
			break
		}
		// Add jitter: up to +25% of delay to spread out concurrent retries.
		jitter := time.Duration(rand.Int64N(int64(delay / 4)))
		actualDelay := delay + jitter
		slog.Warn(p.tag()+": transient error, retrying",
			"operation", operation,
			"attempt", attempt+1,
			"max_retries", profile.maxRetries,
			"delay", actualDelay,
			"error", lastErr,
		)
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %s retry cancelled: %w (last error: %v)", p.tag(), operation, ctx.Err(), lastErr)
		case <-time.After(actualDelay):
		}
		delay = min(delay*2, profile.maxDelay)
	}
	return fmt.Errorf("%s failed after %d retries: %w", operation, profile.maxRetries, lastErr)
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
