package learning

import (
	"context"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
)

func (s *Service) OnMemoryCommitted(ctx context.Context, text, source string, outcome capmemory.AddOutcome) {
	if outcome == capmemory.AddOutcomeDuplicate {
		return
	}
	_ = s.recordMemorySignal(ctx, text, source)
	s.pruneDreamingSignals(ctx, text)
}
