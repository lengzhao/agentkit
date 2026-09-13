package memory

import (
	"context"
	"fmt"
	"strings"

	rtmem "github.com/lengzhao/agentkit/runtime/memory"
)

func (s *Service) CaptureMemoryAdd(ctx context.Context, text, source string) (string, error) {
	if s.memoryWriteRequiresApproval(ctx, source) {
		return s.stageMemory(ctx, rtmem.StagedActionAdd, "", text, source)
	}
	return s.addMemory(ctx, text, source)
}

func (s *Service) CaptureMemoryReplace(ctx context.Context, oldText, content, source string) (string, error) {
	if s.memoryWriteRequiresApproval(ctx, source) {
		return s.stageMemory(ctx, rtmem.StagedActionReplace, oldText, content, source)
	}
	out, err := s.memoryToolReplace(ctx, oldText, content, source)
	if err != nil {
		return "", err
	}
	if !out.Success {
		return "", fmt.Errorf("%s", out.Error)
	}
	return out.Message, nil
}

func (s *Service) CaptureMemoryRemove(ctx context.Context, oldText string) (string, error) {
	oldText = strings.TrimSpace(oldText)
	if oldText == "" {
		return "", fmt.Errorf("old_text is required")
	}
	if s.memoryWriteRequiresApproval(ctx, "background-review") {
		return s.stageMemory(ctx, rtmem.StagedActionRemove, oldText, "", "background-review")
	}
	return s.removeMemory(ctx, oldText, "background-review")
}
