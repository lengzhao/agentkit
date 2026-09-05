package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"strings"
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
	// Inject lists fields prepended to each inbound user message as
	// [meta sender_id=... timestamp="..." task_id="..." ...].
	// Built-in tokens: sender_id, sender_name, sender_email, platform, chat_id,
	// timestamp, task_id, trace_id, language, custom.*, or any Metadata key.
	// L0 config.base.yaml defaults to sender_id, sender_name, timestamp; inject: [] disables.
	Inject []string `json:"inject"`
	// DefaultTimezone is the fallback IANA timezone for inject timestamp when the
	// platform does not implement UserTimezoneProvider.
	DefaultTimezone string `json:"defaultTimezone"`
}

type Deps struct {
	Platform     agentkit.Platform         `json:"platform"`
	Loop         agentkit.Loop             `json:"loop"`
	SessionStore agentkit.SessionStore     `json:"sessionStore,omitempty"`
	Schedules    []capschedule.Runtime     `json:"schedules,omitempty"`
	Init         []agentkit.AppInitializer `json:"init,omitempty"`
	Telemetry    telemetry.Exporter        `json:"telemetry,omitempty"`
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
	inject          []string
	defaultTimezone string
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
		inject:          normalizeInjectAllowlist(cfg.Inject),
		defaultTimezone: strings.TrimSpace(cfg.DefaultTimezone),
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
	if err := runAppInit(ctx, result); err != nil {
		return err
	}
	sched := newScheduler(r.maxConcurrent, r.dispatch, r.reportTurnError)
	submit := r.inboundSubmit(sched)
	bindSubagentSubmit(result, submit)
	// Let in-flight turns record turn/end before the process goes away.
	defer sched.wait(r.shutdownTimeout)

	runtimes := attachScheduleRuntimes(ctx, r, sched)
	slog.Info("runner ready",
		"session_scope", r.sessionScope,
		"max_concurrent_turns", r.maxConcurrent,
		"schedule_runtimes", len(runtimes),
		"shutdown_timeout", r.shutdownTimeout.String(),
	)

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
	deliveryID := session.DeliveryFromEnvelope(req.Event.Envelope)
	conversation := session.ConversationFromLoopRequest(req)
	slog.Error("loop dispatch failed",
		"session_id", conversation,
		"delivery_session_id", deliveryID,
		"agent_id", req.Event.AgentID,
		"err", err,
	)
	out := session.OutboundFromEnvelope(req.Event.Envelope, "error", json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())))
	out.AgentID = req.Event.AgentID
	if out.PlatformID == "" {
		out.PlatformID = req.Event.PlatformID
	}
	if out.UserID == "" {
		out.UserID = req.Event.UserID
	}
	if sendErr := r.platform.Send(ctx, out); sendErr != nil {
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
			"session_id", session.ConversationFromLoopRequest(req),
			"delivery_session_id", session.DeliveryFromEnvelope(req.Event.Envelope),
			"agent_id", req.Event.AgentID,
			"panic", fmt.Sprint(recovered),
			"stack", string(debug.Stack()),
		)
		err = fmt.Errorf("turn panicked: %v", recovered)
	}()
	return r.loop.Dispatch(ctx, req)
}

func attachCommands(result *build.Result) error {
	err := build.WireContributions(
		result,
		func(collector agentkit.CommandCollector, providers []agentkit.CommandProvider) error {
			return collector.SetCommands(providers)
		},
	)
	if errors.Is(err, build.ErrNoContributionsCollector) {
		// Headless platforms (worker, timer, cron) do not wire a commands registry.
		return nil
	}
	return err
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

// inboundPermissionCapability resolves platform permission behavior for one inbound
// event. Schedule-fired turns stay non-interactive even when delivery routes back
// to an interactive platform (e.g. chat-api for send), so cron jobs never block on
// ask_user.
func inboundPermissionCapability(platform agentkit.Platform, event agentkit.MessageEvent) permission.Capability {
	cap := permissionCapability(platform, event.PlatformID)
	if capschedule.IsFireTurn(event.Metadata) {
		cap.Interactive = false
	}
	return cap
}

var _ agentkit.Runner = (*Root)(nil)
