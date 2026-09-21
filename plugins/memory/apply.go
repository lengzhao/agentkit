package memory

import (
	"context"
	"fmt"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

func (s *Service) applyMemoryAdd(ctx context.Context, text, source string) (*rtmem.MemoryStore, rtmem.MemoryAddResult, error, error) {
	store, err := s.memoryStore(ctx)
	if err != nil {
		return nil, rtmem.MemoryAddResult{}, nil, err
	}
	addRes, err := store.Add(ctx, text)
	if err != nil {
		return store, rtmem.MemoryAddResult{}, nil, err
	}
	var commitErr error
	if addRes.Outcome != capmemory.AddOutcomeDuplicate {
		commitErr = s.commitAfterMemoryAdd(ctx, text, source, addRes.Outcome)
	}
	return store, addRes, commitErr, nil
}

func (s *Service) addMemory(ctx context.Context, text, source string) (string, error) {
	store, addRes, commitErr, err := s.applyMemoryAdd(ctx, text, source)
	if err != nil {
		return "", err
	}
	if addRes.Outcome == capmemory.AddOutcomeDuplicate {
		return "memory unchanged (duplicate)", nil
	}
	warning := ""
	if commitErr != nil {
		warning = fmt.Sprintf("\nwarning: post-commit hook failed: %v", commitErr)
	}
	entries, err := store.Load(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("personal memory updated [%d/%d chars]%s", store.TotalChars(entries), store.CharLimit, warning), nil
}
