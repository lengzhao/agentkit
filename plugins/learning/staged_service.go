package learning

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lengzhao/agentkit/plugins/learning/workshop"
	rtlearning "github.com/lengzhao/agentkit/runtime/learning"
)

func (s *Service) stagedStore(ctx context.Context) (*rtlearning.StagedStore, error) {
	dir, err := s.workspace.Resolve(ctx, stagedRelPath(s.memoryRoot))
	if err != nil {
		return nil, err
	}
	return &rtlearning.StagedStore{Dir: dir}, nil
}

func (s *Service) reviewQuotaStore(ctx context.Context) (*rtlearning.QuotaStore, error) {
	path, err := s.workspace.Resolve(ctx, reviewQuotaRelPath(s.memoryRoot))
	if err != nil {
		return nil, err
	}
	return &rtlearning.QuotaStore{Path: path}, nil
}

func (s *Service) stageMemory(ctx context.Context, text, source string) (string, error) {
	store, err := s.stagedStore(ctx)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	entry := rtlearning.StagedMemory{
		ID:        id,
		Content:   text,
		Source:    source,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Add(entry); err != nil {
		return "", err
	}
	return fmt.Sprintf("memory staged for approval (id %s)", id), nil
}

func (s *Service) listPending(ctx context.Context) (string, error) {
	store, err := s.stagedStore(ctx)
	if err != nil {
		return "", err
	}
	entries, err := store.List()
	if err != nil {
		return "", err
	}
	wsStore, _, err := s.workshopStore(ctx)
	if err != nil {
		return "", err
	}
	proposals, err := wsStore.List()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if len(entries) == 0 && len(proposals) == 0 {
		return "no pending memory or skill proposals", nil
	}
	if len(entries) > 0 {
		b.WriteString("staged memory:\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "  %s [%s] %s\n", e.ID, e.Source, truncateDisplay(e.Content, 120))
		}
	}
	hasSkill := false
	for _, p := range proposals {
		if p.Meta.Status == workshop.StatusPending {
			hasSkill = true
			break
		}
	}
	if hasSkill {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("skill proposals: use /learn workshop list\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (s *Service) approveStaged(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("usage: /learn approve <id>")
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
	return s.addMemory(ctx, entry.Content, entry.Source+"-approved")
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
		if _, err := s.addMemory(ctx, e.Content, e.Source+"-approved"); err != nil {
			return fmt.Sprintf("approved %d, then failed: %v", n, err), nil
		}
		n++
	}
	if err := store.Clear(); err != nil {
		return fmt.Sprintf("approved %d entries but failed to clear staged file: %v", n, err), nil
	}
	return fmt.Sprintf("approved %d staged memory entries", n), nil
}

func (s *Service) rejectStaged(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("usage: /learn reject <id>")
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

func truncateDisplay(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
