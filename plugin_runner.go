package agentkit

import (
	"context"

	"github.com/lengzhao/pluginkit/build"
)

// Runner is the root plugin type. It owns process lifecycle and connects a
// Platform to a Loop.
type Runner interface {
	// Run starts the process. result is the build graph produced alongside this
	// runner; implementations may wire CommandProvider contributions to
	// commands/registry before serving traffic.
	Run(context.Context, *build.Result) error
	Stop(context.Context) error
}

// Platform adapts external transports into AgentKit message events. Every
// inbound MessageEvent must carry a delivery SessionID (finest grain); runner
// applies sessionScope for scheduling and history. OutboundEvent.SessionID must
// be the delivery id so replies reach the correct IM target.
//
// Concurrency contract:
//   - Receive is called from a single goroutine and may block.
//   - Send must be safe for concurrent use. Runner runs turns from different
//     effective sessions in parallel (runner.config.maxConcurrentTurns, default
//     64); each turn emits from its own goroutine.
type Platform interface {
	Receive(context.Context) (MessageEvent, error)
	Send(context.Context, OutboundEvent) error
}

// PlatformIdentifier exposes the stable routing ID for a leaf platform.
// Multiplex and other aggregators use it to key sub-platforms without extra
// config; conflicts are disambiguated with an index suffix.
type PlatformIdentifier interface {
	PlatformID() string
}

// UserTimezoneProvider is optional on leaf platforms. Runner uses it when
// runner.config.inject includes timestamp.
type UserTimezoneProvider interface {
	UserTimezone(userID string) string
}
