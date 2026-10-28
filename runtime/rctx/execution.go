package rctx

import (
	"context"
	"github.com/lengzhao/agentkit"
	capexecution "github.com/lengzhao/agentkit/cap/execution"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type executionKey struct{}

// Execution is the immutable history/cwd snapshot prepared for one turn.
// Its defined type name is retained for runtime/reflection compatibility.
// Epoch is a durable context version, not an invocation ID.
type Execution struct {
	SessionID agentkit.SessionID
	Cwd       string
	Epoch     string
}

// SessionContext names the history snapshot separately from invocation identity.
// It aliases the original defined Execution type, preserving its runtime name.
type SessionContext = Execution

func WithSessionContext(ctx context.Context, execution SessionContext) context.Context {
	return context.WithValue(workspace.WithWorkingDirectory(ctx, execution.Cwd), executionKey{}, execution)
}
func SessionContextFromContext(ctx context.Context) (SessionContext, bool) {
	value, ok := ctx.Value(executionKey{}).(SessionContext)
	return value, ok
}

// WithoutSessionContext clears a previous turn snapshot while retaining route and
// workspace identity. The next turn resolves pending bindings again.
func WithoutSessionContext(ctx context.Context) context.Context {
	return context.WithValue(workspace.WithWorkingDirectory(ctx, ""), executionKey{}, SessionContext{})
}

// WithExecution preserves the legacy session-context writer.
func WithExecution(ctx context.Context, execution Execution) context.Context {
	return WithSessionContext(ctx, execution)
}

// ExecutionFromContext preserves the legacy session-context reader.
func ExecutionFromContext(ctx context.Context) (Execution, bool) {
	return SessionContextFromContext(ctx)
}

// WithoutExecution preserves the legacy session-context reset.
func WithoutExecution(ctx context.Context) context.Context { return WithoutSessionContext(ctx) }

type executionIdentityKey struct{}

func WithExecutionIdentity(ctx context.Context, identity capexecution.Identity) context.Context {
	return context.WithValue(ctx, executionIdentityKey{}, identity)
}
func ExecutionIdentityFromContext(ctx context.Context) capexecution.Identity {
	identity, _ := ctx.Value(executionIdentityKey{}).(capexecution.Identity)
	return identity
}

type inheritedWorkingDirectoryKey struct{}

// WithInheritedWorkingDirectory captures a parent's turn directory as the
// child's default. It survives clearing an execution snapshot and never copies
// the parent's history epoch into the child session.
func WithInheritedWorkingDirectory(ctx context.Context, cwd string) context.Context {
	return context.WithValue(ctx, inheritedWorkingDirectoryKey{}, cwd)
}
func InheritedWorkingDirectory(ctx context.Context) string {
	cwd, _ := ctx.Value(inheritedWorkingDirectoryKey{}).(string)
	return cwd
}
