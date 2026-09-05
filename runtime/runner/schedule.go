package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/telemetry"
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

func (r *Root) routePolicy(event agentkit.MessageEvent) session.RoutePolicy {
	return session.RoutePolicyForPlatform(event.PlatformID, session.DefaultRoutePolicy(r.sessionScope))
}

func (r *Root) handleInbound(ctx context.Context, sched *scheduler, event agentkit.MessageEvent) {
	policy := r.routePolicy(event)
	env := session.ResolveEnvelope(event, policy)
	ctx = session.ApplyEnvelopeToContext(ctx, env)
	conversation, err := r.resolveConversation(ctx, event, env, policy)
	if err != nil {
		r.reportInboundError(ctx, env, event, err)
		return
	}
	env = env.WithConversation(conversation)
	ctx = session.ApplyEnvelopeToContext(ctx, env)
	scoped := session.SyncMessageEvent(event, env)
	agentID, err := r.resolveAgentID(ctx, scoped, agentkit.SessionID(conversation))
	if err != nil {
		r.reportInboundError(ctx, env, event, err)
		return
	}
	if agentID != "" {
		scoped.AgentID = agentID
		env = env.WithAgentID(agentID)
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
	if r.loop.IsSessionBusy(agentkit.SessionID(conversation)) {
		steerMsg := r.formatInboundEvent(scoped, env).Message
		slog.Info("inbound steered to busy session",
			"platform", event.PlatformID,
			"user_id", env.Actor.UserID,
			"route", routeLogID(env.Route),
			"conversation", conversation,
			"workspace", env.Workspace,
			"agent_id", scoped.AgentID,
			"preview", telemetry.SummarizeMessage(steerMsg),
		)
		if err := r.loop.Steer(ctx, steerMsg); err != nil {
			r.reportInboundError(ctx, env, scoped, err)
		}
		return
	}
	scoped = r.formatInboundEvent(scoped, env)
	slog.Info("inbound turn queued",
		"platform", event.PlatformID,
		"user_id", env.Actor.UserID,
		"route", routeLogID(env.Route),
		"conversation", conversation,
		"workspace", env.Workspace,
		"agent_id", scoped.AgentID,
		"preview", telemetry.SummarizeMessage(scoped.Message),
	)
	fmt.Fprintln(os.Stderr)
	emit := func(ctx context.Context, out agentkit.OutboundEvent) error {
		out.Route = env.Route
		if out.AgentID == "" {
			out.AgentID = scoped.AgentID
		}
		if out.PlatformID == "" {
			out.PlatformID = env.Route.Platform
		}
		if out.UserID == "" {
			out.UserID = env.Actor.UserID
		}
		if err := out.RequirePlatformID(); err != nil {
			return err
		}
		return r.platform.Send(ctx, out)
	}
	sched.submit(ctx, agentkit.LoopRequest{
		Event:      scoped,
		Emit:       emit,
		Capability: inboundPermissionCapability(r.platform, scoped),
	})
}

func routeLogID(route agentkit.RouteRef) string {
	if id, ok := session.RouteSessionID(route); ok {
		return string(id)
	}
	return string(route.Kind)
}

func (r *Root) reportInboundError(ctx context.Context, env agentkit.TurnEnvelope, event agentkit.MessageEvent, err error) {
	slog.Error("inbound routing failed",
		"conversation", env.Conversation,
		"route", routeLogID(env.Route),
		"err", err,
	)
	out := session.OutboundFromEnvelope(env, "error", json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())))
	out.AgentID = event.AgentID
	_ = r.platform.Send(ctx, out)
}

func (r *Root) reportScheduleError(_ context.Context, err error) {
	slog.Error("schedule runtime failed", "err", err)
}
