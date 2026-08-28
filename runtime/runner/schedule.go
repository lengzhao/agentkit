package runner

import (
	"context"
	"fmt"
	"os"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
)

func attachScheduleRuntimes(ctx context.Context, r *Root, sched *scheduler) []capschedule.Runtime {
	runtimes := r.schedules
	if len(runtimes) == 0 {
		return nil
	}
	submit := r.inboundSubmit(sched)
	for _, rt := range runtimes {
		if rt == nil {
			continue
		}
		go func(rt capschedule.Runtime) {
			if err := rt.Start(ctx, submit); err != nil && ctx.Err() == nil {
				r.reportScheduleError(ctx, err)
			}
		}(rt)
	}
	return runtimes
}

func (r *Root) inboundSubmit(sched *scheduler) capschedule.SubmitFunc {
	return func(ctx context.Context, event agentkit.MessageEvent) error {
		r.handleInbound(ctx, sched, event)
		return nil
	}
}

func (r *Root) handleInbound(ctx context.Context, sched *scheduler, event agentkit.MessageEvent) {
	deliveryID, _, scoped := r.scopedEvent(event)
	if r.loop.TryDeliverPermission(scoped) {
		return
	}
	if event.Message.Role != "" {
		r.loop.SupersedePendingForInbound(scoped)
	}
	if event.Message.Role == "" {
		return
	}
	fmt.Fprintln(os.Stderr)
	emit := func(ctx context.Context, out agentkit.OutboundEvent) error {
		out.SessionID = deliveryID
		if out.AgentID == "" {
			out.AgentID = event.AgentID
		}
		if out.PlatformID == "" {
			out.PlatformID = event.PlatformID
		}
		if out.UserID == "" {
			out.UserID = event.UserID
		}
		return r.platform.Send(ctx, out)
	}
	sched.submit(ctx, agentkit.LoopRequest{
		Event:             scoped,
		DeliverySessionID: deliveryID,
		Emit:              emit,
		Capability:        permissionCapability(r.platform, event.PlatformID),
	})
}

func (r *Root) reportScheduleError(ctx context.Context, err error) {
	_ = r.platform.Send(ctx, agentkit.OutboundEvent{
		Type: "error",
		Data: fmt.Appendf(nil, `{"error":%q}`, err.Error()),
	})
}
