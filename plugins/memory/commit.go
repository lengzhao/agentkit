package memory

import (
	"context"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

func (s *Service) commitAfterMemoryAdd(ctx context.Context, text, source string, outcome capmemory.AddOutcome) error {
	if outcome == capmemory.AddOutcomeDuplicate {
		return nil
	}
	action := "add"
	if outcome == capmemory.AddOutcomeReplaced {
		action = "replace"
	}
	s.appendMemoryLedger(ctx, rtmem.MemoryLedgerEvent{
		Action:  action,
		Content: text,
		Source:  source,
	})
	s.notifyCommitted(ctx, text, source, outcome)
	return nil
}

func (s *Service) commitAfterMemoryReplace(ctx context.Context, content, source string) {
	s.appendMemoryLedger(ctx, rtmem.MemoryLedgerEvent{
		Action:  "replace",
		Content: content,
		Source:  source,
	})
	s.notifyCommitted(ctx, content, source, capmemory.AddOutcomeReplaced)
}

func (s *Service) commitAfterMemoryRemove(ctx context.Context, removed, match, source string) {
	s.appendMemoryLedger(ctx, rtmem.MemoryLedgerEvent{
		Action:  "remove",
		Content: removed,
		Match:   match,
		Source:  source,
	})
	s.notifyCommitted(ctx, removed, source, capmemory.AddOutcomeRemoved)
}

func (s *Service) notifyCommitted(ctx context.Context, text, source string, outcome capmemory.AddOutcome) {
	if len(s.observers) == 0 {
		return
	}
	for _, o := range s.observers {
		o.OnMemoryCommitted(ctx, text, source, outcome)
	}
}

func (s *Service) appendMemoryLedger(ctx context.Context, ev rtmem.MemoryLedgerEvent) {
	ledger, err := s.memoryLedger(ctx)
	if err != nil {
		return
	}
	_ = ledger.Append(ctx, ev)
}
