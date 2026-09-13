package memory

import (
	"context"
	"fmt"
	"strings"

	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

func (s *Service) memoryPolicyStore(ctx context.Context) (*rtmem.MemoryPolicyStore, error) {
	path, err := s.workspace.Resolve(ctx, rtmem.MemoryPolicyRel(s.memoryRoot))
	if err != nil {
		return nil, err
	}
	return &rtmem.MemoryPolicyStore{Path: path}, nil
}

func (s *Service) effectiveMemoryWrite(ctx context.Context) (string, bool) {
	store, err := s.memoryPolicyStore(ctx)
	if err != nil {
		return defaultMemoryWriteMode(s.review), false
	}
	p, err := store.Load()
	if err != nil {
		return defaultMemoryWriteMode(s.review), false
	}
	if p.MemoryWrite != "" {
		return p.MemoryWrite, true
	}
	return defaultMemoryWriteMode(s.review), false
}

func defaultMemoryWriteMode(review ReviewConfig) string {
	if review.WriteApproval != nil && *review.WriteApproval {
		return "approve"
	}
	return "auto"
}

// BackgroundReviewRequiresStaging implements cap/memory.Service.
func (s *Service) BackgroundReviewRequiresStaging(ctx context.Context) bool {
	mode, _ := s.effectiveMemoryWrite(ctx)
	return mode == "approve"
}

func (s *Service) memoryWriteRequiresApproval(ctx context.Context, source string) bool {
	if source != "background-review" {
		return false
	}
	return s.BackgroundReviewRequiresStaging(ctx)
}

func (s *Service) handleMemoryPolicy(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		mode, fromFile := s.effectiveMemoryWrite(ctx)
		return rtmem.FormatMemoryPolicyStatus(mode, fromFile), nil
	}
	switch strings.ToLower(args[0]) {
	case "show", "status":
		return s.handleMemoryPolicy(ctx, nil)
	case "reset":
		store, err := s.memoryPolicyStore(ctx)
		if err != nil {
			return "", err
		}
		if err := store.Save(rtmem.MemoryWritePolicy{}); err != nil {
			return "", err
		}
		return "memory policy reset; using config defaults again", nil
	case "approve", "auto", "approval", "automatic", "授权", "自动", "需要授权":
		return s.setMemoryPolicy(ctx, args)
	default:
		return "", fmt.Errorf("usage: /memory policy [show]|approve|auto|reset")
	}
}

func (s *Service) setMemoryPolicy(ctx context.Context, args []string) (string, error) {
	raw := ""
	if len(args) > 0 {
		raw = args[0]
	}
	mode := normalizeMemoryWriteMode(raw)
	if mode == "" {
		return "", fmt.Errorf("memory policy must be approve or auto")
	}
	store, err := s.memoryPolicyStore(ctx)
	if err != nil {
		return "", err
	}
	if err := store.Save(rtmem.MemoryWritePolicy{MemoryWrite: mode}); err != nil {
		return "", err
	}
	if mode == "approve" {
		return "background-review memory will be staged until /memory approve", nil
	}
	return "background-review memory will write memory.md directly", nil
}

func normalizeMemoryWriteMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "approve", "approval", "auth", "authorized", "需要授权", "授权":
		return "approve"
	case "auto", "automatic", "自动":
		return "auto"
	default:
		return ""
	}
}
