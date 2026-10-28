// Package execution defines backend-independent execution identity and ownership.
package execution

import "context"

// Identity identifies one invocation, independently of its reusable history or
// the backend's telemetry turn. RootTurnID is the owning user interaction.
type Identity struct {
	ID         string `json:"id"`
	ParentID   string `json:"parentId,omitempty"`
	RootTurnID string `json:"rootTurnId"`
}

// Registrar registers cancellation with an explicit owner. Releasing a
// registration only retires that registration, never its parent or siblings.
type Registrar interface {
	RegisterExecution(context.Context, Identity) (context.Context, func())
}
