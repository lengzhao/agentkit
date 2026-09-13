package memory

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) CaptureMemoryAdd(ctx context.Context, text, source string) (string, error) {
	if s.memoryWriteRequiresApproval(ctx, source) {
		return s.stageMemory(ctx, text, source)
	}
	return s.addMemory(ctx, text, source)
}

func (s *Service) CaptureMemoryReplace(ctx context.Context, oldText, content, source string) (string, error) {
	if s.memoryWriteRequiresApproval(ctx, source) {
		combined := strings.TrimSpace(oldText) + stagedReplaceSep + strings.TrimSpace(content)
		return s.stageMemory(ctx, combined, source)
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
		return s.stageMemory(ctx, stagedRemovePrefix+oldText, "background-review")
	}
	return s.removeMemory(ctx, oldText, "background-review")
}
