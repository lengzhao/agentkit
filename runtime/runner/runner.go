package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

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
	return &Root{platform: deps.Platform, loop: deps.Loop}, nil
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
		err = r.loop.Dispatch(ctx, agentkit.LoopRequest{Event: event, Emit: emit})
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
