package memory

import (
	"context"
	"strings"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

// PreviewAddOutcome implements cap/memory.Reader.
func (s *Service) PreviewAddOutcome(ctx context.Context, text string) (capmemory.AddOutcome, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return capmemory.AddOutcomeDuplicate, nil
	}
	entries, _, _, err := s.LoadEntries(ctx)
	if err != nil {
		return "", err
	}
	rtEntries := make([]rtmem.MemoryEntry, len(entries))
	for i, e := range entries {
		rtEntries[i] = rtmem.MemoryEntry{Content: e.Content, Meta: e.Meta}
	}
	_, outcome := rtmem.MergeMemoryAdd(rtEntries, text)
	return outcome, nil
}
