package learning

import (
	"context"
	"strings"
	"time"

	"github.com/lengzhao/agentkit/plugins/learning/dreaming"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
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
	st, err := stateStore.Load()
	if err != nil || st == nil || len(st.Signals) == 0 {
		return ""
	}
	entries, err := s.loadMemoryEntries(ctx)
	if err != nil {
		return ""
	}
	filtered := filterSignalsNotInMemory(st.Signals, entries)
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

func (s *Service) loadMemoryEntries(ctx context.Context) ([]rtmem.MemoryEntry, error) {
	capEntries, _, _, err := s.memory.LoadEntries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]rtmem.MemoryEntry, len(capEntries))
	for i, e := range capEntries {
		out[i] = rtmem.MemoryEntry{Content: e.Content, Meta: e.Meta}
	}
	return out, nil
}

func filterSignalsNotInMemory(signals []dreaming.Signal, entries []rtmem.MemoryEntry) []dreaming.Signal {
	if len(signals) == 0 {
		return nil
	}
	out := make([]dreaming.Signal, 0, len(signals))
	for _, sig := range signals {
		text := strings.TrimSpace(sig.Text)
		if text == "" {
			continue
		}
		_, outcome := rtmem.MergeMemoryAdd(entries, text)
		if outcome == capmemory.AddOutcomeDuplicate {
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
	st, err := stateStore.Load()
	if err != nil || st == nil {
		return
	}
	st.PruneByText(text)
	_ = stateStore.Save(st)
}
