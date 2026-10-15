package llm

import (
	"context"
	"errors"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestIsRetryableError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("rate limit exceeded"), true},
		{errors.New("connection lost"), true},
		{errors.New("unexpected end of JSON input"), true},
		{errors.New("unexpected EOF"), true},
		{&openai.APIError{HTTPStatusCode: 503, Message: "overloaded"}, true},
		{&openai.APIError{HTTPStatusCode: 429, Message: "insufficient_quota"}, false},
		{&openai.APIError{HTTPStatusCode: 404, Message: "Model 'deepseek-v4-flash' is not available in this group."}, false},
		{errors.New("context length exceeded"), false},
		{errors.New("billing issue"), false},
		{context.DeadlineExceeded, false},
		{errors.New("Client.Timeout exceeded while awaiting headers"), false},
	}
	for _, tc := range cases {
		if got := IsRetryableError(tc.err); got != tc.want {
			t.Fatalf("IsRetryableError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestIsContextOverflowError(t *testing.T) {
	t.Parallel()
	if !IsContextOverflowError(errors.New("maximum context length exceeded")) {
		t.Fatal("expected overflow")
	}
	if IsContextOverflowError(errors.New("rate limit")) {
		t.Fatal("rate limit is not overflow")
	}
}

func TestIsSafetyError(t *testing.T) {
	t.Parallel()

	// 线上真实报错（trace 373cf049）：400 + flagged by safety checks。
	flagged := &openai.APIError{HTTPStatusCode: 400, Message: "The input text was flagged by safety checks. Please modify your request and try again."}
	if !IsSafetyError(flagged) {
		t.Fatal("expected safety error for flagged input")
	}
	for _, msg := range []string{
		"content_filter",
		"data_inspection_failed",
		"violates content policy",
	} {
		if !IsSafetyError(errors.New(msg)) {
			t.Fatalf("expected safety error for %q", msg)
		}
	}
	if IsSafetyError(errors.New("rate limit exceeded")) {
		t.Fatal("rate limit is not a safety error")
	}
	if IsSafetyError(errors.New("maximum context length exceeded")) {
		t.Fatal("overflow is not a safety error")
	}
	// 安全错误不可按瞬态重试（相同 payload 重试必然复发）。
	if IsRetryableError(flagged) {
		t.Fatal("safety errors must not be retryable")
	}
}

func TestIsQuotaError(t *testing.T) {
	t.Parallel()
	if !IsQuotaError(errors.New("insufficient_quota")) {
		t.Fatal("expected quota error")
	}
	if IsQuotaError(errors.New("rate limit exceeded")) {
		t.Fatal("rate limit is not quota")
	}
}
