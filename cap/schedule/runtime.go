package schedule

import (
	"context"

	"github.com/lengzhao/agentkit"
)

// SubmitFunc delivers one inbound turn. Runner provides this when starting a
// Runtime so due jobs enter the same path as platform messages.
type SubmitFunc = func(ctx context.Context, event agentkit.MessageEvent) error

// Runtime watches a Registry and submits due jobs as inbound turns. It is
// started by runner after build; it must not depend on runner in the plugin graph.
type Runtime interface {
	Start(context.Context, SubmitFunc) error
	Stop(context.Context) error
}
