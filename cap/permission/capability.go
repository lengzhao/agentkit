package permission

import "time"

type AnswerScope string

const (
	ScopeAsker  AnswerScope = "asker"
	ScopeAnyone AnswerScope = "anyone"
)

// Capability describes how a platform renders and collects permission replies.
type Capability struct {
	Interactive    bool
	MultiSelect    bool
	DefaultTimeout time.Duration
	AnswerScope    AnswerScope
}

// Capable is implemented by leaf platforms that can render permission requests.
type Capable interface {
	PermissionCapability() Capability
}

// CapabilityRouter is implemented by platforms that aggregate multiple leaf platforms.
type CapabilityRouter interface {
	PermissionCapabilityFor(platformID string) Capability
}
