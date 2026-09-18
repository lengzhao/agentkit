package subagent

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/rctx"
)

func (p parentContext) asyncRunContext() context.Context {
	ctx := context.Background()
	ctx = rctx.ApplyEnvelopeToContext(ctx, p.envelope)
	ctx = rctx.WithAgentID(ctx, p.agentID)
	if p.sessionControl != nil {
		ctx = context.WithValue(ctx, agentkit.KeySessionControl, p.sessionControl)
	}
	ctx = context.WithValue(ctx, agentkit.KeyAsyncSubagent, true)
	ctx = rctx.ContextWithOutboundEmit(ctx, p.emit)
	return ctx
}
