package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/runtime/session"
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
	deliveryID, effectiveID, scoped := r.scopedEvent(event)
	ctx = withInboundRoutingContext(ctx, effectiveID, deliveryID)
	storeSessionID, err := r.resolveStoreSessionID(ctx, event, effectiveID)
	if err != nil {
		r.reportInboundError(ctx, deliveryID, event, err)
		return
	}
	agentID, err := r.resolveAgentID(ctx, event, storeSessionID)
	if err != nil {
		r.reportInboundError(ctx, deliveryID, event, err)
		return
	}
	if agentID != "" {
		scoped.AgentID = agentID
	}
	if r.loop.TryDeliverPermission(scoped) {
		return
	}
	if event.Message.Role != "" {
		r.loop.SupersedePendingForInbound(scoped)
	}
	if event.Message.Role == "" {
		return
	}
	if r.loop.IsSessionBusy(effectiveID) {
		if err := r.loop.Steer(ctx, event.Message); err != nil {
			r.reportInboundError(ctx, deliveryID, scoped, err)
		}
		return
	}
	fmt.Fprintln(os.Stderr)
	emit := func(ctx context.Context, out agentkit.OutboundEvent) error {
		out.SessionID = deliveryID
		if out.AgentID == "" {
			out.AgentID = scoped.AgentID
		}
		if out.PlatformID == "" {
			out.PlatformID = event.PlatformID
		}
		if out.UserID == "" {
			out.UserID = event.UserID
		}
		if out.PlatformID == "" {
			if p := session.ParseDelivery(deliveryID, out.UserID).Platform; p != "" {
				out.PlatformID = p
			}
		}
		if err := out.RequirePlatformID(); err != nil {
			return err
		}
		return r.platform.Send(ctx, out)
	}
	sched.submit(ctx, agentkit.LoopRequest{
		Event:             scoped,
		DeliverySessionID: deliveryID,
		StoreSessionID:    storeSessionID,
		Emit:              emit,
		Capability:        permissionCapability(r.platform, event.PlatformID),
	})
}

func withInboundRoutingContext(ctx context.Context, effectiveID, deliveryID agentkit.SessionID) context.Context {
	if effectiveID != "" {
		ctx = context.WithValue(ctx, agentkit.KeySessionID, effectiveID)
	}
	if deliveryID != "" {
		ctx = context.WithValue(ctx, agentkit.KeyDeliverySessionID, deliveryID)
	}
	return ctx
}

func (r *Root) reportInboundError(ctx context.Context, deliveryID agentkit.SessionID, event agentkit.MessageEvent, err error) {
	slog.Error("inbound routing failed",
		"session_id", event.SessionID,
		"delivery_session_id", deliveryID,
		"err", err,
	)
	_ = r.platform.Send(ctx, agentkit.OutboundEvent{
		SessionID:  deliveryID,
		AgentID:    event.AgentID,
		PlatformID: event.PlatformID,
		UserID:     event.UserID,
		Type:       "error",
		Data:       json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())),
	})
}

func (r *Root) reportScheduleError(ctx context.Context, err error) {
	slog.Error("schedule runtime failed", "err", err)
}
