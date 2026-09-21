package learning

import (
	"context"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/plugins/learning/dreaming"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
)

// ReviewSignalCandidates builds a digest block from dreaming short-term signals for background review.
func (s *Service) ReviewSignalCandidates(ctx context.Context) string {
	cfg := s.dreamingCfg()
	if !cfg.FeedReviewEnabled() {
		return ""
	}
	stateStore, err := s.dreamingStore(ctx)
	if err != nil {
		return ""
	}
	st, err := stateStore.Load(ctx)
	if err != nil || st == nil || len(st.Signals) == 0 {
		return ""
	}
	filtered := s.filterSignalsNotInMemory(ctx, st.Signals)
	if len(filtered) == 0 {
		return ""
	}
	now := time.Now().UTC()
	norm := cfg.Normalized()
	topK := norm.FeedReviewTopK
	if topK <= 0 {
		topK = 8
	}
	top := dreaming.TopScoredCandidates(filtered, cfg, now, topK)
	return dreaming.FormatReviewCandidateBlock(top)
}

func (s *Service) filterSignalsNotInMemory(ctx context.Context, signals []dreaming.Signal) []dreaming.Signal {
	if len(signals) == 0 {
		return nil
	}
	out := make([]dreaming.Signal, 0, len(signals))
	for _, sig := range signals {
		text := strings.TrimSpace(sig.Text)
		if text == "" {
			continue
		}
		outcome, err := s.memory.PreviewAddOutcome(ctx, text)
		if err != nil || outcome == capmemory.AddOutcomeDuplicate {
			continue
		}
		out = append(out, sig)
	}
	return out
}

func (s *Service) pruneDreamingSignals(ctx context.Context, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	stateStore, err := s.dreamingStore(ctx)
	if err != nil {
		return
	}
	st, err := stateStore.Load(ctx)
	if err != nil || st == nil {
		return
	}
	st.PruneByText(text)
	_ = stateStore.Save(ctx, st)
}
