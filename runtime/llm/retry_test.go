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

func TestIsQuotaError(t *testing.T) {
	t.Parallel()
	if !IsQuotaError(errors.New("insufficient_quota")) {
		t.Fatal("expected quota error")
	}
	if IsQuotaError(errors.New("rate limit exceeded")) {
		t.Fatal("rate limit is not quota")
	}
}
