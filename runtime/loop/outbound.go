package loop

import (
	"context"
	"encoding/json"

	"github.com/lengzhao/agentkit"
)

// OutboundEmitFromContext returns the per-turn outbound hook, if any.
func OutboundEmitFromContext(ctx context.Context) agentkit.OutboundEmit {
	emit, _ := ctx.Value(agentkit.KeyOutboundEmit).(agentkit.OutboundEmit)
	return emit
}

// ContextWithOutboundEmit attaches the per-turn outbound hook to ctx.
func ContextWithOutboundEmit(ctx context.Context, emit agentkit.OutboundEmit) context.Context {
	return context.WithValue(ctx, agentkit.KeyOutboundEmit, emit)
}

// MarshalOutboundData JSON-encodes an outbound event payload.
func MarshalOutboundData(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
