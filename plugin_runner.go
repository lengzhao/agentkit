package agentkit

import "context"

// Runner is the root plugin type. It owns process lifecycle and connects a
// Platform to a Loop.
type Runner interface {
	Run(context.Context) error
	Stop(context.Context) error
}

// Platform adapts external transports into AgentKit message events.
type Platform interface {
	Receive(context.Context) (MessageEvent, error)
	Send(context.Context, OutboundEvent) error
}
