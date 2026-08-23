package agentkit

import (
	"context"

	"github.com/lengzhao/pluginkit/build"
)

// Runner is the root plugin type. It owns process lifecycle and connects a
// Platform to a Loop.
type Runner interface {
	// Run starts the process. result is the build graph produced alongside this
	// runner; the implementation may collect optional capabilities such as slash
	// commands from it before serving traffic.
	Run(context.Context, *build.Result) error
	Stop(context.Context) error
}

// Platform adapts external transports into AgentKit message events. Every
// inbound MessageEvent must carry a stable SessionID; every outbound
// OutboundEvent must echo the same SessionID so replies reach the correct
// conversation. SessionID generation and delivery-target encoding are platform-
// specific; Loop and Agent treat SessionID as opaque.
type Platform interface {
	Receive(context.Context) (MessageEvent, error)
	Send(context.Context, OutboundEvent) error
}

// ReplyTargetResolver is an optional platform capability for proactive sends
// (cron, push, cc-connect send) where no live inbound message exists. The
// platform reconstructs its private reply/delivery context from an opaque
// SessionID. Only platform plugins implement this; Loop and Agent must not
// parse SessionID segments.
type ReplyTargetResolver interface {
	ResolveReplyTarget(SessionID) (replyCtx any, err error)
}
