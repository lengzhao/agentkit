package agentkit

import "context"

// Loop is the turn scheduler. It routes inbound events to agents and decides
// when a turn or step should continue.
type Loop interface {
	Dispatch(context.Context, LoopRequest) (LoopResult, error)
}

type LoopRequest struct {
	Event MessageEvent
}

type LoopResult struct {
	Outbound []OutboundEvent
}
