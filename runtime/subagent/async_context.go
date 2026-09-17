package subagent

import (
	"context"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/session"
)

func (p parentContext) asyncRunContext() context.Context {
	ctx := context.Background()
	ctx = session.ApplyEnvelopeToContext(ctx, p.envelope)
	ctx = session.WithAgentID(ctx, p.agentID)
	if p.sessionControl != nil {
		ctx = context.WithValue(ctx, agentkit.KeySessionControl, p.sessionControl)
	}
	ctx = context.WithValue(ctx, agentkit.KeyAsyncSubagent, true)
	ctx = loop.ContextWithOutboundEmit(ctx, p.emit)
	return ctx
}
