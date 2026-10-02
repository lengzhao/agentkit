package learning

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	caplearning "github.com/lengzhao/agentkit/cap/learning"
	capschedule "github.com/lengzhao/agentkit/cap/schedule"
	cw "github.com/lengzhao/agentkit/cap/workspace"
)

// DreamSweep runs scheduled dreaming sweeps without agent turns.
type DreamSweep struct {
	svc  caplearning.DreamSweepScheduler
	poll time.Duration
	wait func(context.Context, time.Duration) error
}

type DreamSweepConfig struct {
	PollSeconds int `json:"pollSeconds"`
}

type DreamSweepDeps struct {
	Learning caplearning.DreamSweepScheduler `json:"learning"`
}

// SetDefaults implements pluginkit.Defaulter.
func (c *DreamSweepConfig) SetDefaults() {
	if c.PollSeconds == 0 {
		c.PollSeconds = 60
	}
}

// Validate implements pluginkit.Validator.
func (c *DreamSweepConfig) Validate() error {
	if c.PollSeconds <= 0 {
		return fmt.Errorf("learning/dream-sweep pollSeconds must be positive")
	}
	return nil
}

// NewDreamSweep registers learning/dream-sweep: background grounded dreaming sweeps.
func NewDreamSweep(cfg DreamSweepConfig, deps DreamSweepDeps) (capschedule.Runtime, error) {
	if deps.Learning == nil {
		return nil, fmt.Errorf("learning/dream-sweep requires learning dependency")
	}
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	poll := time.Duration(cfg.PollSeconds) * time.Second
	return &DreamSweep{
		svc:  deps.Learning,
		poll: poll,
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
		if d.svc != nil && !d.svc.Disabled() {
			if err := d.runDueSweeps(ctx); err != nil {
				slog.Warn("dream sweep failed", "err", err)
			}
		}
		if err := d.wait(ctx, d.poll); err != nil {
			return err
		}
	}
}

func (d *DreamSweep) Stop(context.Context) error { return nil }

func (d *DreamSweep) runDueSweeps(ctx context.Context) error {
	walker, ok := d.svc.Workspace().(cw.LocalTenantWalker)
	if !ok {
		if !d.svc.DreamSweepDue(ctx) {
			return nil
		}
		return d.svc.RunScheduledDreamSweep(ctx)
	}
	return walker.WalkLocalTenants(ctx, func(tctx context.Context) error {
		if !d.svc.DreamSweepDue(tctx) {
			return nil
		}
		return d.svc.RunScheduledDreamSweep(tctx)
	})
}
