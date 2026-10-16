package feishu

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

var errTestTransient = errors.New("response content-type not json, StatusCode: 403")

// TestTransientRetryProfilesFailFastVsStandard：quick 档（中途流式帧）必须快速
// 放弃——只重试 1 次；standard 档重试 2 次。中途帧丢了下帧会补齐，多重重试只
// 会在出口网关故障期持续锤网关并长时间占用同卡写锁。
func TestTransientRetryProfilesFailFastVsStandard(t *testing.T) {
	p := &Platform{}

	var quickCalls atomic.Int32
	start := time.Now()
	err := p.withTransientRetryProfile(context.Background(), "op", retryProfileQuick, func() error {
		quickCalls.Add(1)
		return errTestTransient
	})
	if err == nil {
		t.Fatal("expected error after quick profile exhausts retries")
	}
	if got := quickCalls.Load(); got != 2 {
		t.Fatalf("quick profile should attempt exactly 2 times (1 retry), got %d", got)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("quick profile should fail fast, took %v", elapsed)
	}

	var stdCalls atomic.Int32
	err = p.withTransientRetryProfile(context.Background(), "op", retryProfileStandard, func() error {
		stdCalls.Add(1)
		return errTestTransient
	})
	if err == nil {
		t.Fatal("expected error after standard profile exhausts retries")
	}
	if got := stdCalls.Load(); got != 3 {
		t.Fatalf("standard profile should attempt exactly 3 times (2 retries), got %d", got)
	}
}

// TestTransientRetrySucceedsAfterTransient：transient 错误后重试成功应返回 nil。
func TestTransientRetrySucceedsAfterTransient(t *testing.T) {
	p := &Platform{}
	var calls atomic.Int32
	err := p.withTransientRetryProfile(context.Background(), "op", retryProfileQuick, func() error {
		if calls.Add(1) == 1 {
			return errTestTransient
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil after retry succeeds, got %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

// TestFinalizeProfileShortButAboveStandard：定档只比 standard 多覆盖短暂抖动
// （3 次重试、~4s），且必须保持短预算——长故障窗口下快速进入降级链（新最终卡
// → 纯文本兜底），最终投递保证靠纯文本的 5s/20s/60s 长重试，而不是让卡片路径
// 在故障期长时间重试锤网关。
func TestFinalizeProfileShortButAboveStandard(t *testing.T) {
	if retryProfileFinalize.maxRetries <= retryProfileStandard.maxRetries {
		t.Fatalf("finalize profile must retry more than standard: %d vs %d",
			retryProfileFinalize.maxRetries, retryProfileStandard.maxRetries)
	}
	delay := transientRetryInitial
	var coverage time.Duration
	for i := 0; i < retryProfileFinalize.maxRetries; i++ {
		coverage += delay
		delay = min(delay*2, retryProfileFinalize.maxDelay)
	}
	if coverage > 10*time.Second {
		t.Fatalf("finalize profile backoff coverage %v too long; should fall through to fallback chain fast", coverage)
	}
}
