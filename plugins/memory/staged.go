package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

func (s *Service) stageMemory(ctx context.Context, action, oldText, content, source string) (string, error) {
	store, err := s.stagedStore(ctx)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	entry := rtmem.StagedMemory{
		ID:        id,
		Action:    strings.TrimSpace(action),
		OldText:   strings.TrimSpace(oldText),
		Content:   strings.TrimSpace(content),
		Source:    source,
		CreatedAt: time.Now().UTC(),
	}
	if entry.Action == "" {
		return "", fmt.Errorf("staged action is required")
	}
	if err := store.Add(entry); err != nil {
		return "", err
	}
	return fmt.Sprintf("memory staged for approval (id %s)", id), nil
}

func (s *Service) ListStaged(ctx context.Context) ([]capmemory.StagedEntry, error) {
	store, err := s.stagedStore(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := store.List()
	if err != nil {
		return nil, err
	}
	out := make([]capmemory.StagedEntry, len(entries))
	for i, e := range entries {
		out[i] = capmemory.StagedEntry{
			ID:      e.ID,
			Source:  e.Source,
			Content: e.Content,
			Action:  e.Action,
			OldText: e.OldText,
		}
	}
	return out, nil
}

func (s *Service) ApproveStaged(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("usage: /memory approve <id>")
	}
	if strings.EqualFold(id, "all") {
		return s.approveAllStaged(ctx)
	}
	store, err := s.stagedStore(ctx)
	if err != nil {
		return "", err
	}
	entry, err := store.Remove(id)
	if err != nil {
		return "", err
	}
	return s.applyStagedEntry(ctx, entry, entry.Source+"-approved")
}

func (s *Service) RejectStaged(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("usage: /memory reject <id>")
	}
	store, err := s.stagedStore(ctx)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(id, "all") {
		entries, err := store.List()
		if err != nil {
			return "", err
		}
		if len(entries) == 0 {
			return "no staged memory to reject", nil
		}
		if err := store.Clear(); err != nil {
			return "", err
		}
		return fmt.Sprintf("rejected %d staged memory entries", len(entries)), nil
	}
	if _, err := store.Remove(id); err != nil {
		return "", err
	}
	return "staged memory rejected", nil
}

func (s *Service) approveAllStaged(ctx context.Context) (string, error) {
	store, err := s.stagedStore(ctx)
	if err != nil {
		return "", err
	}
	entries, err := store.List()
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "no staged memory to approve", nil
	}
	n := 0
	for _, e := range entries {
		if _, err := s.applyStagedEntry(ctx, e, e.Source+"-approved"); err != nil {
			return fmt.Sprintf("approved %d, then failed: %v", n, err), nil
		}
		n++
	}
	if err := store.Clear(); err != nil {
		return fmt.Sprintf("approved %d entries but failed to clear staged file: %v", n, err), nil
	}
	return fmt.Sprintf("approved %d staged memory entries", n), nil
}

func (s *Service) applyStagedEntry(ctx context.Context, entry rtmem.StagedMemory, source string) (string, error) {
	entry = rtmem.NormalizeStagedEntry(entry)
	action := strings.TrimSpace(entry.Action)
	if action == "" {
		return "", fmt.Errorf("staged content is empty")
	}
	switch action {
	case rtmem.StagedActionAdd:
		if strings.TrimSpace(entry.Content) == "" {
			return "", fmt.Errorf("staged add content is empty")
		}
		return s.addMemory(ctx, entry.Content, source)
	case rtmem.StagedActionReplace:
		old := strings.TrimSpace(entry.OldText)
		newText := strings.TrimSpace(entry.Content)
		if old == "" || newText == "" {
			return "", fmt.Errorf("staged replace requires old_text and content")
		}
		baseSource := strings.TrimSuffix(source, "-approved")
		msg, err := s.CaptureMemoryReplace(ctx, old, newText, baseSource)
		if err != nil {
			return "", err
		}
		return msg, nil
	case rtmem.StagedActionRemove:
		old := strings.TrimSpace(entry.OldText)
		if old == "" {
			return "", fmt.Errorf("staged remove missing old_text")
		}
		return s.removeMemory(ctx, old, source)
	default:
		return "", fmt.Errorf("unknown staged action %q", action)
	}
}
