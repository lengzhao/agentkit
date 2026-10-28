package rctx

import (
	"context"
	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/workspace"
)

type executionKey struct{}

// Execution identifies one session's immutable turn context. Epoch is the
// durable session/context identity, used by stateful remote adapters.
type Execution struct {
	SessionID agentkit.SessionID
	Cwd       string
	Epoch     string
}

func WithExecution(ctx context.Context, execution Execution) context.Context {
	return context.WithValue(workspace.WithWorkingDirectory(ctx, execution.Cwd), executionKey{}, execution)
}
func ExecutionFromContext(ctx context.Context) (Execution, bool) {
	value, ok := ctx.Value(executionKey{}).(Execution)
	return value, ok
}

// WithoutExecution clears a previous turn snapshot while retaining route and
// workspace identity. The next turn resolves pending bindings again.
func WithoutExecution(ctx context.Context) context.Context {
	return context.WithValue(workspace.WithWorkingDirectory(ctx, ""), executionKey{}, Execution{})
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
