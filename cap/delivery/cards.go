package delivery

import (
	"context"
	"fmt"

	"github.com/lengzhao/agentkit"
)

// CardSender is an optional platform capability for custom plugins. It works
// without an agent turn and reuses the platform's credentials and card renderer.
type CardSender interface {
	SendCard(context.Context, agentkit.RouteRef, *Card) (MessageRef, error)
}

// MessageRef identifies the posted card. Persist the entire value, including the
// existing platform Instance, if a plugin needs to retain the reference.
type MessageRef struct {
	Route agentkit.RouteRef `json:"route"`
	ID    string            `json:"id"`
}

type ErrorCode string

const (
	CodeUnsupported   ErrorCode = "unsupported"
	CodeInvalidTarget ErrorCode = "invalid_target"
	CodeInvalidCard   ErrorCode = "invalid_card"
	CodeForbidden     ErrorCode = "forbidden"
	CodeRateLimited   ErrorCode = "rate_limited"
	CodeTransport     ErrorCode = "transport"
	CodeFailed        ErrorCode = "failed"
)

// Unknown means the card may have been posted. A timeout is not proof that it
// was not sent. Adapters never automatically retry uncertain writes.
type Error struct {
	Code    ErrorCode
	Unknown bool
	Cause   error
}

func (e *Error) Error() string {
	if e.Unknown {
		return fmt.Sprintf("card delivery: %s (result unknown)", e.Code)
	}
	return fmt.Sprintf("card delivery: %s", e.Code)
}
func (e *Error) Unwrap() error { return e.Cause }
