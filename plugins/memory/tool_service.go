package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

func (s *Service) MemoryTool(ctx context.Context, in capmemory.MemoryToolInput) (capmemory.MemoryToolOutput, error) {
	if s.disabled {
		return capmemory.MemoryToolOutput{Success: false, Error: "memory is disabled"}, nil
	}
	action := rtmem.NormalizeMemoryToolAction(in.Action)
	switch action {
	case "add":
		return s.memoryToolAdd(ctx, strings.TrimSpace(in.Content), "memory-tool")
	case "replace":
		return s.memoryToolReplace(ctx, strings.TrimSpace(in.OldText), strings.TrimSpace(in.Content), "memory-tool")
	case "remove":
		return s.memoryToolRemove(ctx, strings.TrimSpace(in.OldText))
	default:
		return capmemory.MemoryToolOutput{Success: false, Error: "action must be add, replace, or remove"}, nil
	}
}

func (s *Service) memoryToolAdd(ctx context.Context, text, source string) (capmemory.MemoryToolOutput, error) {
	if text == "" {
		return capmemory.MemoryToolOutput{Success: false, Error: "content is required for add"}, nil
	}
	store, addRes, commitErr, err := s.applyMemoryAdd(ctx, text, source)
	if capErr := memoryCapacityOutput(store, err); capErr != nil {
		return *capErr, nil
	}
	if err != nil {
		return capmemory.MemoryToolOutput{Success: false, Error: err.Error()}, nil
	}
	if addRes.Outcome == capmemory.AddOutcomeDuplicate {
		usage, _ := memoryToolSnapshot(store)
		return capmemory.MemoryToolOutput{Success: true, Message: "no duplicate added", Usage: usage}, nil
	}
	if commitErr != nil {
		return capmemory.MemoryToolOutput{}, commitErr
	}
	usage, _ := memoryToolSnapshot(store)
	return memoryToolOKf(store, "memory updated [%s]", usage), nil
}

func (s *Service) memoryToolReplace(ctx context.Context, oldText, content, source string) (capmemory.MemoryToolOutput, error) {
	if oldText == "" {
		return capmemory.MemoryToolOutput{Success: false, Error: "old_text is required for replace"}, nil
	}
	if content == "" {
		return capmemory.MemoryToolOutput{Success: false, Error: "content is required for replace"}, nil
	}
	store, err := s.memoryStore(ctx)
	if err != nil {
		return capmemory.MemoryToolOutput{}, err
	}
	_, err = store.Replace(oldText, content)
	if capErr := memoryCapacityOutput(store, err); capErr != nil {
		return *capErr, nil
	}
	if err != nil {
		return capmemory.MemoryToolOutput{Success: false, Error: err.Error()}, nil
	}
	s.commitAfterMemoryReplace(ctx, content, source)
	usage, _ := memoryToolSnapshot(store)
	return memoryToolOKf(store, "memory entry replaced [%s]", usage), nil
}

func (s *Service) memoryToolRemove(ctx context.Context, oldText string) (capmemory.MemoryToolOutput, error) {
	if oldText == "" {
		return capmemory.MemoryToolOutput{Success: false, Error: "old_text is required for remove"}, nil
	}
	msg, err := s.removeMemory(ctx, oldText, "memory-tool")
	if err != nil {
		return capmemory.MemoryToolOutput{Success: false, Error: err.Error()}, nil
	}
	store, err := s.memoryStore(ctx)
	if err != nil {
		return capmemory.MemoryToolOutput{}, err
	}
	return memoryToolOK(store, msg), nil
}

func (s *Service) removeMemory(ctx context.Context, text, source string) (string, error) {
	store, err := s.memoryStore(ctx)
	if err != nil {
		return "", err
	}
	removed, err := store.Remove(text)
	if err != nil {
		return "", err
	}
	s.commitAfterMemoryRemove(ctx, removed, text, source)
	return "personal memory entry removed", nil
}

func memoryToolSnapshot(store *rtmem.MemoryStore) (usage string, entries []string) {
	loaded, err := store.Load()
	if err != nil {
		return rtmem.FormatMemoryUsage(0, store.CharLimit), nil
	}
	used := store.TotalChars(loaded)
	usage = rtmem.FormatMemoryUsage(used, store.CharLimit)
	return usage, rtmem.EntryContents(loaded)
}

func memoryToolOK(store *rtmem.MemoryStore, message string) capmemory.MemoryToolOutput {
	usage, entries := memoryToolSnapshot(store)
	return capmemory.MemoryToolOutput{
		Success:        true,
		Message:        message,
		Usage:          usage,
		CurrentEntries: entries,
	}
}

func memoryToolOKf(store *rtmem.MemoryStore, format string, args ...any) capmemory.MemoryToolOutput {
	return memoryToolOK(store, fmt.Sprintf(format, args...))
}

func memoryCapacityOutput(store *rtmem.MemoryStore, err error) *capmemory.MemoryToolOutput {
	var capErr *rtmem.MemoryAtCapacityError
	if !errors.As(err, &capErr) || capErr == nil {
		return nil
	}
	used, limit := 0, 0
	if store != nil {
		used, limit = store.TotalChars(capErr.Entries), store.CharLimit
	} else if capErr.Limit > 0 {
		used, limit = capErr.Used, capErr.Limit
	}
	return &capmemory.MemoryToolOutput{
		Success:        false,
		Error:          capErr.Error(),
		CurrentEntries: rtmem.EntryContents(capErr.Entries),
		Usage:          rtmem.FormatMemoryUsage(used, limit),
	}
}
