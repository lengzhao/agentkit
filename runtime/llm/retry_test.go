package llm

import (
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
		{&openai.APIError{HTTPStatusCode: 503, Message: "overloaded"}, true},
		{&openai.APIError{HTTPStatusCode: 429, Message: "insufficient_quota"}, false},
		{errors.New("context length exceeded"), false},
		{errors.New("billing issue"), false},
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
