package learning

import (
	"context"
	"strings"
	"time"

	caplearning "github.com/lengzhao/agentkit/cap/learning"
	"github.com/lengzhao/agentkit/cap/workspace"
)

var (
	_ caplearning.ReviewHost          = (*Service)(nil)
	_ caplearning.DreamSweepScheduler = (*Service)(nil)
)

func (s *Service) Disabled() bool { return s.disabled }

func (s *Service) Workspace() workspace.Service { return s.workspace }

func (s *Service) SessionsDir() string { return s.sessionsDir }

func (s *Service) ReviewNudgeLoad(ctx context.Context, sessionID string) (caplearning.NudgeSessionState, bool, error) {
	store, err := s.reviewNudgeStore(ctx)
	if err != nil {
		return caplearning.NudgeSessionState{}, false, err
	}
	return store.LoadSession(sessionID)
}

func (s *Service) ReviewNudgeSave(ctx context.Context, sessionID string, state caplearning.NudgeSessionState) error {
	store, err := s.reviewNudgeStore(ctx)
	if err != nil {
		return err
	}
	return store.SaveSession(sessionID, state)
}

func (s *Service) RunScheduledDreamSweep(ctx context.Context) error {
	_, err := s.runDreamSweep(ctx)
	return err
}

func (s *Service) DreamSweepDue(ctx context.Context) bool {
	store, err := s.dreamingStore(ctx)
	if err != nil {
		return false
	}
	st, err := store.Load()
	if err != nil || st == nil || !st.Enabled {
		return false
	}
	cfg := s.dreamingCfg()
	expr := strings.TrimSpace(cfg.Frequency)
	if expr == "" {
		expr = "0 3 * * *"
	}
	sched, err := s.engine.ParseCron(expr)
	if err != nil {
		return false
	}
	now := time.Now().UTC()
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
