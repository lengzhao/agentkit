package interaction

import (
	"context"
	"fmt"
)

// CardResponder lets a consuming plugin close its source card without importing
// a platform SDK. Closing changes presentation only; plugin state stays owned by
// that plugin. An empty text uses the normalized question and submitted answers.
type CardResponder interface {
	CloseCard(context.Context, string) error
}
type cardResponderKey struct{}

func WithCardResponder(ctx context.Context, r CardResponder) context.Context {
	return context.WithValue(ctx, cardResponderKey{}, r)
}
func CloseCard(ctx context.Context, text string) error {
	r, ok := ctx.Value(cardResponderKey{}).(CardResponder)
	if !ok {
		return fmt.Errorf("interaction card response unavailable")
	}
	return r.CloseCard(ctx, text)
}

type Purpose string

const (
	PurposeSubmit   Purpose = "submit"
	PurposeChange   Purpose = "change"
	PurposeNavigate Purpose = "navigation"
)

type SubmissionMode string

const (
	SubmissionOnce    SubmissionMode = "once"
	SubmissionPerUser SubmissionMode = "per_user"
	SubmissionRepeat  SubmissionMode = "repeat"
)
