package tools

import (
	"context"

	"github.com/lengzhao/agentkit"
)

type scopeKey struct{}

func WithScope(ctx context.Context, scope agentkit.ToolScope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

func scopeFrom(ctx context.Context) agentkit.ToolScope {
	if v, ok := ctx.Value(scopeKey{}).(agentkit.ToolScope); ok {
		return v
	}
	return agentkit.ToolScope{}
}
