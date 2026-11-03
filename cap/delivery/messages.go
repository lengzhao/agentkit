package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/lengzhao/agentkit"
)

// Part is platform-neutral content. Text and user mentions are supported by the
// initial IM adapters. IDs are opaque, scoped to the target platform instance.
// Unsupported types must be rejected before any message is published.
type Part struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	UserID string `json:"userId,omitempty"`
}

// MessageRef identifies one physical message. Persist the entire value, including
// Route.Instance; a protocol name alone cannot distinguish bot installations.
type MessageRef struct {
	Route agentkit.RouteRef `json:"route"`
	ID    string            `json:"id"`
}

type Outcome string

const (
	OutcomeNotSent   Outcome = "not_sent"
	OutcomeConfirmed Outcome = "confirmed"
	OutcomePartial   Outcome = "partial"
	// Unknown means the operation may have reached the platform. It is not a
	// safe-retry instruction. Messages still contains every confirmed reference.
	OutcomeUnknown Outcome = "unknown"
)

type ErrorCode string

const (
	CodeUnsupported    ErrorCode = "unsupported"
	CodeInvalidTarget  ErrorCode = "invalid_target"
	CodeInvalidContent ErrorCode = "invalid_content"
	CodeForbidden      ErrorCode = "forbidden"
	CodeRateLimited    ErrorCode = "rate_limited"
	CodeTransport      ErrorCode = "transport"
	CodeFailed         ErrorCode = "failed"
)

// Error separates the reason from delivery certainty. Error() intentionally
// excludes SDK responses and content; inspect the wrapped error only in trusted
// diagnostics. RetryAfter is advisory and does not make an unknown write safe.
type Error struct {
	Code       ErrorCode
	Outcome    Outcome
	RetryAfter time.Duration
	Cause      error
}

func (e *Error) Error() string { return fmt.Sprintf("delivery: %s (%s)", e.Code, e.Outcome) }
func (e *Error) Unwrap() error { return e.Cause }

type Call struct {
	Caller      string            `json:"caller,omitempty"`
	OperationID string            `json:"operationId,omitempty"`
	Operation   string            `json:"operation,omitempty"`
	Route       agentkit.RouteRef `json:"route"`
}

// Admission is a host policy hook, separate from agent tool permissions and
// business ownership checks. Nil means the host trusts the injected consumer.
type Admission interface {
	AuthorizeDelivery(context.Context, Call) error
}

type PublishRequest struct {
	Route   agentkit.RouteRef `json:"route"`
	Content []Part            `json:"content"`
	Caller  string            `json:"caller,omitempty"`
	// Stable for the same operation AND content. Never reuse for different writes.
	OperationID string `json:"operationId,omitempty"`
}

type Deduplication struct {
	// Mode is "native" only when the adapter documents a bounded native guarantee;
	// "none" does not promise deduplication, even if a correlation ID is sent.
	Mode   string        `json:"mode"`
	Window time.Duration `json:"window,omitempty"`
}

type Receipt struct {
	Route         agentkit.RouteRef `json:"route"`
	Messages      []MessageRef      `json:"messages,omitempty"`
	Outcome       Outcome           `json:"outcome"`
	Deduplication Deduplication     `json:"deduplication"`
	// FailedPart is the zero-based physical part whose result is represented by
	// the error. Parts before it are confirmed; parts after it were not attempted.
	FailedPart *int `json:"failedPart,omitempty"`
}

type EditRequest struct {
	Message     MessageRef `json:"message"`
	Content     []Part     `json:"content"`
	Caller      string     `json:"caller,omitempty"`
	OperationID string     `json:"operationId,omitempty"`
}

type ReadRequest struct {
	Message     MessageRef `json:"message"`
	Caller      string     `json:"caller,omitempty"`
	OperationID string     `json:"operationId,omitempty"`
}

type Message struct {
	Ref     MessageRef `json:"ref"`
	Content []Part     `json:"content,omitempty"`
}

// These are independent optional leaf capabilities. No change to Platform.Send
// is needed. Publishing without a thread never requires TopicCreator.
type Publisher interface {
	Publish(context.Context, PublishRequest) (Receipt, error)
}
type Editor interface {
	EditMessage(context.Context, EditRequest) error
}
type Reader interface {
	ReadMessage(context.Context, ReadRequest) (Message, error)
}

// CreateTopic publishes a root and establishes a reply target. The receipt Route
// is the actual usable target, not a promise of a platform-native titled topic.
type TopicCreator interface {
	CreateTopic(context.Context, PublishRequest) (Receipt, error)
}

// Messages is the routed service surface; consumers may inject any narrow subset.
// Unsupported operations return CodeUnsupported, never silently change targets.
type Messages interface {
	Publisher
	Editor
	Reader
	TopicCreator
}
