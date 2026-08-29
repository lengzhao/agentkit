package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	"github.com/lengzhao/agentkit/cap/telemetry"
	"github.com/lengzhao/agentkit/runtime/agent"
	"github.com/lengzhao/agentkit/runtime/loop"
	"github.com/lengzhao/agentkit/runtime/session"
	"github.com/lengzhao/pluginkit/build"
)

const defaultMaxConcurrentTurns = 64

type Config struct {
	// ShutdownTimeoutSeconds bounds how long shutdown waits for in-flight turns
	// to finish. 0 waits indefinitely.
	ShutdownTimeoutSeconds int `json:"shutdownTimeoutSeconds"`
	// SessionScope collapses platform delivery SessionIDs for Loop locking and
	// history: "channel" (default), "thread", or "user".
	SessionScope string `json:"sessionScope"`
	// MaxConcurrentTurns caps how many turns run at once across distinct effective
	// sessions. Defaults to 64. Ordering within one session is always preserved.
	MaxConcurrentTurns int `json:"maxConcurrentTurns"`
}

type Deps struct {
	Platform     agentkit.Platform      `json:"platform"`
	Loop         agentkit.Loop          `json:"loop"`
	SessionStore agentkit.SessionStore  `json:"sessionStore,omitempty"`
	Schedules    []capschedule.Runtime  `json:"schedules,omitempty"`
	Telemetry    telemetry.Exporter     `json:"telemetry,omitempty"`
}

type Root struct {
	platform        agentkit.Platform
	loop            agentkit.Loop
	sessionStore    agentkit.SessionStore
	schedules       []capschedule.Runtime
	telemetry       telemetry.Exporter
	sessionScope    session.SessionScope
	maxConcurrent   int
	shutdownTimeout time.Duration
}

// New registers runner: Root plugin: connects Platform to Loop and owns process lifecycle.
//
// Best practices:
//   - Platforms emit delivery SessionIDs; runner applies sessionScope and agent routing before dispatch.
//   - Ordering within a session is preserved at any concurrency; only cross-session turns overlap.
//   - A panicking or failing turn is reported on its session and never kills the process.
func New(cfg Config, deps Deps) (agentkit.Runner, error) {
	if deps.Platform == nil {
		return nil, fmt.Errorf("runner requires platform")
	}
	if deps.Loop == nil {
		return nil, fmt.Errorf("runner requires loop")
	}
	if cfg.MaxConcurrentTurns < 0 {
		return nil, fmt.Errorf("runner maxConcurrentTurns must not be negative")
	}
	exp := deps.Telemetry
	if exp == nil {
		exp = telemetry.Noop
	}
	maxConcurrent := cfg.MaxConcurrentTurns
	if maxConcurrent == 0 {
		maxConcurrent = defaultMaxConcurrentTurns
	}
	var shutdownTimeout time.Duration
	if cfg.ShutdownTimeoutSeconds > 0 {
		shutdownTimeout = time.Duration(cfg.ShutdownTimeoutSeconds) * time.Second
	}
	return &Root{
		platform:        deps.Platform,
		loop:            deps.Loop,
		sessionStore:    resolveRunnerSessionStore(deps),
		schedules:       deps.Schedules,
		telemetry:       exp,
		sessionScope:    session.ParseScope(cfg.SessionScope),
		maxConcurrent:   maxConcurrent,
		shutdownTimeout: shutdownTimeout,
	}, nil
}

func resolveRunnerSessionStore(deps Deps) agentkit.SessionStore {
	if deps.SessionStore != nil {
		return deps.SessionStore
	}
	ld, ok := deps.Loop.(*loop.Default)
	if !ok {
		return nil
	}
	if store := ld.SessionStore(); store != nil {
		return store
	}
	for _, ag := range ld.Agents() {
		if store := agentSessionStore(ag); store != nil {
			return store
		}
	}
	return nil
}

func agentSessionStore(ag agentkit.Agent) agentkit.SessionStore {
	runtime, ok := ag.(*agent.Runtime)
	if !ok {
		return nil
	}
	return runtime.SessionStore()
}

func (r *Root) Run(ctx context.Context, result *build.Result) error {
	if err := attachCommands(result); err != nil {
		return err
	}
	sched := newScheduler(r.maxConcurrent, r.dispatch, r.reportTurnError)
	// Let in-flight turns record turn/end before the process goes away.
	defer sched.wait(r.shutdownTimeout)

	runtimes := attachScheduleRuntimes(ctx, r, sched)

	recvDone := make(chan error, 1)
	go r.receiveLoop(ctx, sched, recvDone, len(runtimes) > 0)

	err := <-recvDone
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (r *Root) scopedEvent(event agentkit.MessageEvent) (delivery, effective agentkit.SessionID, scoped agentkit.MessageEvent) {
	delivery = event.SessionID
	effective = session.ApplyScope(delivery, r.sessionScope, event.UserID)
	scoped = event
	scoped.SessionID = effective
	return delivery, effective, scoped
}

func deliverySessionID(req agentkit.LoopRequest) agentkit.SessionID {
	if req.DeliverySessionID != "" {
		return req.DeliverySessionID
	}
	return req.Event.SessionID
}

// receiveLoop reads inbound events without holding concurrency slots. Permission
// replies are delivered immediately; new turns are queued for the scheduler.
// When keepAlive is true, platform EOF keeps the process serving schedule runtimes.
func (r *Root) receiveLoop(ctx context.Context, sched *scheduler, done chan<- error, keepAlive bool) {
	for {
		event, err := r.platform.Receive(ctx)
		if err != nil {
			if keepAlive && errors.Is(err, io.EOF) {
				<-ctx.Done()
				done <- ctx.Err()
				return
			}
			done <- err
			return
		}
		r.handleInbound(ctx, sched, event)
	}
}

// reportTurnError surfaces a failed turn on its own session's channel and keeps
// the process serving. A turn failure is never fatal to the runner.
func (r *Root) reportTurnError(ctx context.Context, req agentkit.LoopRequest, err error) {
	deliveryID := deliverySessionID(req)
	slog.Error("loop dispatch failed",
		"session_id", req.Event.SessionID,
		"delivery_session_id", deliveryID,
		"agent_id", req.Event.AgentID,
		"err", err,
	)
	if sendErr := r.platform.Send(ctx, agentkit.OutboundEvent{
		SessionID:  deliveryID,
		AgentID:    req.Event.AgentID,
		PlatformID: req.Event.PlatformID,
		UserID:     req.Event.UserID,
		Type:       "error",
		Data:       json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())),
	}); sendErr != nil {
		slog.Error("reporting turn error failed", "session_id", deliveryID, "err", sendErr)
	}
}

// dispatch turns a panicking turn into an ordinary turn error. A long-running
// process must not die because one tool call hit a nil map: the panic is logged
// with its stack, reported on the session's error channel, and the loop moves to
// the next event. The interrupted turn leaves an unterminated turn/start behind,
// which the agent repairs on its next turn.
func (r *Root) dispatch(ctx context.Context, req agentkit.LoopRequest) (err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		slog.Error("turn panicked",
			"session_id", req.Event.SessionID,
			"delivery_session_id", deliverySessionID(req),
			"agent_id", req.Event.AgentID,
			"panic", fmt.Sprint(recovered),
			"stack", string(debug.Stack()),
		)
		err = fmt.Errorf("turn panicked: %v", recovered)
	}()
	return r.loop.Dispatch(ctx, req)
}

func attachCommands(result *build.Result) error {
	return build.WireContributions(
		result,
		func(collector agentkit.CommandCollector, providers []agentkit.CommandProvider) error {
			return collector.SetCommands(providers)
		},
	)
}

func (r *Root) Stop(ctx context.Context) error {
	if r.telemetry == nil {
		return nil
	}
	return r.telemetry.Shutdown(ctx)
}

// Loop exposes the turn scheduler so RPC/TUI integrations can steer or queue
// follow-ups; agentkit.Loop already carries Steer/FollowUp.
func (r *Root) Loop() agentkit.Loop { return r.loop }

// SessionStore returns the durable session backend used for routing and bindings.
func (r *Root) SessionStore() agentkit.SessionStore { return r.sessionStore }

func permissionCapability(platform agentkit.Platform, platformID string) permission.Capability {
	if router, ok := platform.(permission.CapabilityRouter); ok && platformID != "" {
		return router.PermissionCapabilityFor(platformID)
	}
	if c, ok := platform.(permission.Capable); ok {
		return c.PermissionCapability()
	}
	return permission.Capability{Interactive: false}
}

var _ agentkit.Runner = (*Root)(nil)
