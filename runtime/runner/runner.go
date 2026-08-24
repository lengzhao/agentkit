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

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/pluginkit/build"
)

type Config struct {
	ShutdownTimeoutSeconds int `json:"shutdownTimeoutSeconds"`
}

type Deps struct {
	Platform agentkit.Platform `json:"platform"`
	Loop     agentkit.Loop     `json:"loop"`
}

type Root struct {
	platform agentkit.Platform
	loop     agentkit.Loop
}

func New(cfg Config, deps Deps) (agentkit.Runner, error) {
	if deps.Platform == nil {
		return nil, fmt.Errorf("runner requires platform")
	}
	if deps.Loop == nil {
		return nil, fmt.Errorf("runner requires loop")
	}
	_ = cfg
	return &Root{
		platform: deps.Platform,
		loop:     deps.Loop,
	}, nil
}

func (r *Root) Run(ctx context.Context, result *build.Result) error {
	if err := attachCommands(result); err != nil {
		return err
	}
	for {
		event, err := r.platform.Receive(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if event.Message.Role == "" {
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
			return r.platform.Send(ctx, out)
		}
		err = r.dispatch(ctx, agentkit.LoopRequest{Event: event, Emit: emit})
		if err != nil {
			slog.Error("loop dispatch failed", "err", err)
			if sendErr := r.platform.Send(ctx, agentkit.OutboundEvent{
				SessionID:  event.SessionID,
				AgentID:    event.AgentID,
				PlatformID: event.PlatformID,
				Type:       "error",
				Data:       json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())),
			}); sendErr != nil {
				return sendErr
			}
			continue
		}
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

var _ agentkit.Runner = (*Root)(nil)
