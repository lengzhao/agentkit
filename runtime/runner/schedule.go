package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	capsession "github.com/lengzhao/agentkit/cap/session"
	"github.com/lengzhao/agentkit/runtime/platform/common"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/telemetry"
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

func (r *Root) routePolicy(event agentkit.MessageEvent) rctx.RoutePolicy {
	protocol := event.Envelope.Route.Platform
	if protocol == "" {
		protocol = rctx.ParseDelivery(rctx.InboundDeliveryID(event), event.Envelope.Actor.UserID).Platform
	}
	return rctx.RoutePolicyForPlatform(protocol, rctx.DefaultRoutePolicy(r.sessionScope))
}

func (r *Root) handleInbound(ctx context.Context, sched *scheduler, event agentkit.MessageEvent) {
	policy := r.routePolicy(event)
	env := rctx.ResolveEnvelope(event, policy)
	env = rctx.WithMetadataScope(env, r.sessionScope)
	ctx = rctx.ApplyEnvelopeToContext(ctx, env)
	conversation, err := r.resolveConversation(ctx, event, env, policy)
	if err != nil {
		r.reportInboundError(ctx, env, event, err)
		return
	}
	env = env.WithConversation(conversation)
	ctx = rctx.ApplyEnvelopeToContext(ctx, env)
	scoped := event.WithEnvelope(env)
	agentID, err := r.resolveAgentID(ctx, scoped, agentkit.SessionID(conversation))
	if err != nil {
		r.reportInboundError(ctx, env, event, err)
		return
	}
	if agentID != "" {
		scoped.Envelope.AgentID = agentID
		env = env.WithAgentID(agentID)
	}
	if len(scoped.Reply) > 0 {
		if r.loop.TryDeliverPermission(scoped) {
			return
		}
		slog.Warn("permission reply not delivered",
			"platform", event.Envelope.Route.Instance,
			"user_id", env.Actor.UserID,
			"route", routeLogID(env.Route),
			"conversation", conversation,
		)
		return
	}
	if event.Message.Role != "" {
		r.loop.SupersedePendingForInbound(scoped)
	}
	if event.Message.Role == "" {
		return
	}
	// Async subagent completion is a new logical turn, not an interruption:
	// never steer it into the running turn (the result would be swallowed or
	// interleaved with the parent's ongoing reply). Let it fall through to the
	// normal sched.submit path below; the scheduler's per-session pending queue
	// serializes it — when the parent is busy it queues and runs as the next
	// Run after the current turn ends, when idle it dispatches immediately.
	// This unifies the slow-child (parent idle) and fast-child (parent busy)
	// cases on a single path, and decouples result delivery from the
	// best-effort progress-card I/O.
	subagentComplete := inboundIsSubagentComplete(scoped)
	// A sequential platform may have observed turn/end before loop teardown
	// clears its busy flag. Preserve that input as a new scheduler request.
	queueTurn, _ := scoped.Envelope.Metadata[agentkit.MetadataQueueTurn].(bool)
	if r.loop.IsSessionBusy(agentkit.SessionID(conversation)) && !subagentComplete && !queueTurn {
		if r.tryStopBusyInbound(ctx, scoped) {
			return
		}
		steerMsg := r.formatInboundEvent(scoped, env).Message
		slog.Info("inbound steered to busy session",
			"platform", event.Envelope.Route.Instance,
			"user_id", env.Actor.UserID,
			"route", routeLogID(env.Route),
			"conversation", conversation,
			"workspace", env.Workspace,
			"agent_id", scoped.Envelope.AgentID,
			"preview", telemetry.SummarizeMessage(steerMsg),
		)
		if err := r.loop.Steer(ctx, steerMsg); err != nil {
			r.reportInboundError(ctx, env, scoped, err)
			return
		}
		r.reportTurnSteer(ctx, env, scoped, scoped.Message)
		return
	}
	scoped = r.formatInboundEvent(scoped, env)
	if subagentComplete {
		slog.Info("async subagent follow-up queued",
			"platform", event.Envelope.Route.Instance,
			"user_id", env.Actor.UserID,
			"route", routeLogID(env.Route),
			"conversation", conversation,
			"workspace", env.Workspace,
			"agent_id", scoped.Envelope.AgentID,
			"preview", telemetry.SummarizeMessage(scoped.Message),
		)
	} else {
		slog.Info("inbound turn queued",
			"platform", event.Envelope.Route.Instance,
			"user_id", env.Actor.UserID,
			"route", routeLogID(env.Route),
			"conversation", conversation,
			"workspace", env.Workspace,
			"agent_id", scoped.Envelope.AgentID,
			"preview", telemetry.SummarizeMessage(scoped.Message),
		)
	}
	fmt.Fprintln(os.Stderr)
	emit := func(ctx context.Context, out agentkit.OutboundEvent) error {
		out.Route = env.Route
		if out.AgentID == "" {
			out.AgentID = scoped.Envelope.AgentID
		}
		if out.UserID == "" {
			out.UserID = env.Actor.UserID
		}
		if err := out.RequireDestination(); err != nil {
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
	if id, ok := rctx.RouteSessionID(route); ok {
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
	out := rctx.OutboundFromEnvelope(env, "error", json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())))
	out.AgentID = event.Envelope.AgentID
	_ = r.platform.Send(ctx, out)
}

func (r *Root) reportScheduleError(_ context.Context, err error) {
	slog.Error("schedule runtime failed", "err", err)
}

// reportTurnSteer tells the platform that an inbound message was folded into a
// running turn instead of starting a new one. The event carries the steered
// message's own route, so platforms acknowledge that exact message (Lark swaps
// its in-flight reaction emoji) rather than the turn's trigger message.
// Best effort: a failed send never blocks steering.
func (r *Root) reportTurnSteer(ctx context.Context, env agentkit.TurnEnvelope, event agentkit.MessageEvent, steered agentkit.ModelMessage) {
	data := capsession.TurnSteerData{
		Outcome:   capsession.SteerOutcomeQueued,
		SessionID: string(rctx.ConversationFromEvent(event)),
		AgentID:   string(event.Envelope.AgentID),
		UserID:    env.Actor.UserID,
		Preview:   clampSteerPreview(telemetry.SummarizeMessage(steered)),
	}
	out := rctx.OutboundFromEnvelope(env, agentkit.EventTurnSteer, rctx.MarshalOutboundData(data))
	out.AgentID = event.Envelope.AgentID
	if err := r.platform.Send(ctx, out); err != nil {
		slog.Debug("reporting turn steer failed",
			"platform", event.Envelope.Route.Instance,
			"route", routeLogID(env.Route),
			"err", err,
		)
	}
}

// turnSteerPreviewRunes bounds the excerpt carried on the wire so a pasted
// document does not travel through every platform adapter.
const turnSteerPreviewRunes = 200

// clampSteerPreview trims the steered message summary to turnSteerPreviewRunes.
func clampSteerPreview(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= turnSteerPreviewRunes {
		return s
	}
	return strings.TrimSpace(string(runes[:turnSteerPreviewRunes])) + "…"
}

func (r *Root) tryStopBusyInbound(ctx context.Context, event agentkit.MessageEvent) bool {
	text := inboundPlainText(event.Message)
	name, args, ok := common.ParseSlashCommand(text)
	if !ok || name != "stop" || strings.TrimSpace(args) != "" {
		return false
	}
	out, err := stopCommand{loop: r.loop, store: r.sessionStore}.CommandExec(ctx, "")
	if err != nil {
		slog.Error("stop command failed",
			"platform", event.Envelope.Route.Instance,
			"user_id", event.Envelope.Actor.UserID,
			"err", err,
		)
		return true
	}
	if out != "" {
		slog.Info("stop command", "platform", event.Envelope.Route.Instance, "reply", out)
	}
	return true
}

func inboundPlainText(msg agentkit.ModelMessage) string {
	var b strings.Builder
	for _, part := range msg.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func inboundIsSubagentComplete(event agentkit.MessageEvent) bool {
	if event.Envelope.Metadata == nil {
		return false
	}
	v, ok := event.Envelope.Metadata["subagent_complete"].(bool)
	return ok && v
}
