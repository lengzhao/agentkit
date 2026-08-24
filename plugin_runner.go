package agentkit

import (
	"context"

	"github.com/lengzhao/pluginkit/build"
)

// Runner is the root plugin type. It owns process lifecycle and connects a
// Platform to a Loop.
type Runner interface {
	// Run starts the process. result is the build graph produced alongside this
	// runner; implementations may collect CommandProvider instances and attach
	// them to commands/registry before serving traffic.
	Run(context.Context, *build.Result) error
	Stop(context.Context) error
}

// Platform adapts external transports into AgentKit message events. Every
// inbound MessageEvent must carry a stable SessionID; every outbound
// OutboundEvent must echo the same SessionID so replies reach the correct
// conversation. SessionID generation and delivery-target encoding are platform-
// specific; Loop and Agent treat SessionID as opaque.
//
// Concurrency contract:
//   - Receive is called from a single goroutine and may block.
//   - Send must be safe for concurrent use. Runner can run turns from different
//     sessions in parallel (runner config maxConcurrentTurns), and each turn
//     emits from its own goroutine.
type Platform interface {
	Receive(context.Context) (MessageEvent, error)
	Send(context.Context, OutboundEvent) error
}
