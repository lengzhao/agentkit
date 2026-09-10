package session

import (
	"context"

	"github.com/lengzhao/agentkit/cap/workspace"
)

type workspaceServiceKey struct{}

// WithWorkspaceService attaches the workspace resolver used for session spill files.
func WithWorkspaceService(ctx context.Context, ws workspace.Service) context.Context {
	if ws == nil {
		return ctx
	}
	return context.WithValue(ctx, workspaceServiceKey{}, ws)
}

// WorkspaceServiceFromContext returns the workspace service for spill I/O, if set.
func WorkspaceServiceFromContext(ctx context.Context) workspace.Service {
	ws, _ := ctx.Value(workspaceServiceKey{}).(workspace.Service)
	return ws
}
