package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"time"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/interaction"
	"github.com/lengzhao/pluginkit/build"
)

type Config struct {
	// ShutdownTimeoutSeconds bounds how long shutdown waits for in-flight turns
	// to finish. 0 waits indefinitely.
	ShutdownTimeoutSeconds int `json:"shutdownTimeoutSeconds"`
	// MaxConcurrentTurns caps how many turns run at once. Defaults to 1, i.e.
	// fully serial, because turns from different sessions share one workspace:
	// two agents running `go build` or editing the same file concurrently is a
	// real hazard. Raise it for multi-conversation transports (IM, HTTP) where
	// sessions are genuinely independent.
	//
	// Ordering within a session is always preserved, whatever the value.
	MaxConcurrentTurns int `json:"maxConcurrentTurns"`
}

type Deps struct {
	Platform agentkit.Platform `json:"platform"`
	Loop     agentkit.Loop     `json:"loop"`
}

type Root struct {
	platform        agentkit.Platform
	loop            agentkit.Loop
	maxConcurrent   int
	shutdownTimeout time.Duration
}

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
	maxConcurrent := cfg.MaxConcurrentTurns
	if maxConcurrent == 0 {
		maxConcurrent = 1
	}
	var shutdownTimeout time.Duration
	if cfg.ShutdownTimeoutSeconds > 0 {
		shutdownTimeout = time.Duration(cfg.ShutdownTimeoutSeconds) * time.Second
	}
	return &Root{
		platform:        deps.Platform,
		loop:            deps.Loop,
		maxConcurrent:   maxConcurrent,
		shutdownTimeout: shutdownTimeout,
	}, nil
}

func (r *Root) Run(ctx context.Context, result *build.Result) error {
	if err := attachCommands(result); err != nil {
		return err
	}
	sched := newScheduler(r.maxConcurrent, r.dispatch, r.reportTurnError)
	// Let in-flight turns record turn/end before the process goes away.
	defer sched.wait(r.shutdownTimeout)

	for {
		// Taking the slot before reading bounds read-ahead to the configured
		// concurrency; at 1 this blocks until the previous turn completes.
		if err := sched.acquire(ctx); err != nil {
			return err
		}
		event, err := r.platform.Receive(ctx)
		if err != nil {
			sched.release()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if r.loop.TryDeliverInteraction(event) {
			sched.release()
			continue
		}
		if event.Message.Role == "" {
			sched.release()
			continue
		}
		fmt.Fprintln(os.Stderr)
		emit := func(ctx context.Context, out agentkit.OutboundEvent) error {
			if out.SessionID == "" {
				out.SessionID = event.SessionID
			}
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
			Event:              event,
			Emit:               emit,
			InteractionHandler: interactionHandler(r.platform),
			AsyncInteraction:   asyncInteraction(r.platform),
		})
	}
}

// reportTurnError surfaces a failed turn on its own session's channel and keeps
// the process serving. A turn failure is never fatal to the runner.
func (r *Root) reportTurnError(ctx context.Context, req agentkit.LoopRequest, err error) {
	slog.Error("loop dispatch failed",
		"session_id", req.Event.SessionID,
		"agent_id", req.Event.AgentID,
		"err", err,
	)
	if sendErr := r.platform.Send(ctx, agentkit.OutboundEvent{
		SessionID:  req.Event.SessionID,
		AgentID:    req.Event.AgentID,
		PlatformID: req.Event.PlatformID,
		UserID:     req.Event.UserID,
		Type:       "error",
		Data:       json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())),
	}); sendErr != nil {
		slog.Error("reporting turn error failed", "session_id", req.Event.SessionID, "err", sendErr)
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
			"agent_id", req.Event.AgentID,
			"panic", fmt.Sprint(recovered),
			"stack", string(debug.Stack()),
		)
		err = fmt.Errorf("turn panicked: %v", recovered)
	}()
	return r.loop.Dispatch(ctx, req)
}

func attachCommands(result *build.Result) error {
	if result == nil {
		return nil
	}
	providers := build.Collect[agentkit.CommandProvider](result)
	if len(providers) == 0 {
		return nil
	}
	for _, collector := range build.Collect[agentkit.CommandCollector](result) {
		if collector == nil {
			continue
		}
		if err := collector.SetCommands(providers); err != nil {
			return err
		}
	}
	return nil
}

func (r *Root) Stop(context.Context) error { return nil }

// Loop exposes the turn scheduler so RPC/TUI integrations can steer or queue
// follow-ups; agentkit.Loop already carries Steer/FollowUp.
func (r *Root) Loop() agentkit.Loop { return r.loop }

func interactionHandler(platform agentkit.Platform) interaction.Handler {
	if h, ok := platform.(interaction.Handler); ok {
		return h
	}
	return nil
}

func asyncInteraction(platform agentkit.Platform) bool {
	if a, ok := platform.(interaction.AsyncPlatform); ok {
		return a.AsyncInteraction()
	}
	return false
}

var _ agentkit.Runner = (*Root)(nil)
