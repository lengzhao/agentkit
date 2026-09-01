package learning

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	capschedule "github.com/lengzhao/agentkit/cap/schedule"
)

// DreamSweep runs scheduled dreaming sweeps without agent turns.
type DreamSweep struct {
	svc  *Service
	poll time.Duration
	now  func() time.Time
	wait func(context.Context, time.Duration) error
}

type DreamSweepConfig struct {
	PollSeconds int `json:"pollSeconds"`
}

type DreamSweepDeps struct {
	Learning *Service `json:"learning"`
}

// NewDreamSweep registers learning/dream-sweep: background grounded dreaming sweeps.
func NewDreamSweep(cfg DreamSweepConfig, deps DreamSweepDeps) (capschedule.Runtime, error) {
	if deps.Learning == nil {
		return nil, fmt.Errorf("learning/dream-sweep requires learning dependency")
	}
	poll := time.Duration(cfg.PollSeconds) * time.Second
	if cfg.PollSeconds <= 0 {
		poll = 60 * time.Second
	}
	return &DreamSweep{
		svc:  deps.Learning,
		poll: poll,
		now:  time.Now,
		wait: sleepContext,
	}, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (d *DreamSweep) Start(ctx context.Context, _ capschedule.SubmitFunc) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.svc != nil && d.svc.dreamingEnabled() && d.sweepDue(ctx) {
			if _, err := d.svc.runDreamSweep(ctx); err != nil {
				slog.Warn("dream sweep failed", "err", err)
			}
		}
		if err := d.wait(ctx, d.poll); err != nil {
			return err
		}
	}
}

func (d *DreamSweep) Stop(context.Context) error { return nil }

func (d *DreamSweep) sweepDue(ctx context.Context) bool {
	store, err := d.svc.dreamingStore(ctx)
	if err != nil {
		return false
	}
	st, err := store.Load()
	if err != nil || st == nil || !st.Enabled {
		return false
	}
	cfg := d.svc.dreamingCfg()
	expr := strings.TrimSpace(cfg.Frequency)
	if expr == "" {
		expr = "0 3 * * *"
	}
	sched, err := capschedule.ParseCron(expr)
	if err != nil {
		return false
	}
	now := d.now()
	anchor := st.LastSweep
	if anchor.IsZero() {
		anchor = now.Add(-24 * time.Hour)
	}
	next, ok := sched.Next(anchor)
	if !ok {
		return false
	}
	return !next.After(now)
}
