package subagent

import (
	"context"
	"strings"

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
	if id := strings.TrimSpace(p.turnID); id != "" {
		ctx = context.WithValue(ctx, agentkit.KeyTurnID, id)
	}
	ctx = rctx.ContextWithOutboundEmit(ctx, p.emit)
	return ctx
}
