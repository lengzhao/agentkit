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
	capEntries, _, _, err := s.memory.LoadEntries(ctx)
	if err != nil {
		return ""
	}
	filtered := filterSignalsNotInMemory(st.Signals, capEntries)
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

func filterSignalsNotInMemory(signals []dreaming.Signal, capEntries []capmemory.MemoryEntry) []dreaming.Signal {
	if len(signals) == 0 {
		return nil
	}
	entries := make([]rtmem.MemoryEntry, len(capEntries))
	for i, e := range capEntries {
		entries[i] = rtmem.MemoryEntry{Content: e.Content}
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
