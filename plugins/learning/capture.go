package learning

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
	rtlearning "github.com/lengzhao/agentkit/runtime/learning"
	"github.com/lengzhao/agentkit/plugins/learning/workshop"
	"github.com/lengzhao/agentkit/runtime/session"
)

// CaptureMemoryAdd implements rtlearning.CaptureApplier.
func (s *Service) CaptureMemoryAdd(ctx context.Context, text, source string) (string, error) {
	if s.memoryWriteRequiresApproval(ctx, source) {
		return s.stageMemory(ctx, text, source)
	}
	return s.addMemory(ctx, text, source)
}

// CaptureMemoryRemove implements rtlearning.CaptureApplier.
func (s *Service) CaptureMemoryRemove(ctx context.Context, oldText string) (string, error) {
	return s.removeMemory(ctx, oldText)
}

// CaptureSkillPropose implements rtlearning.CaptureApplier.
func (s *Service) CaptureSkillPropose(ctx context.Context, name, body, sessionID, focus, source string) (string, error) {
	if !s.skillsWorkshopEnabled(ctx) {
		return "", fmt.Errorf("skill workshop is disabled (use /learn policy skills propose|auto)")
	}
	wsStore, skillsDir, err := s.workshopStore(ctx)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(wsStore.Root, 0o755); err != nil {
		return "", err
	}
	pending, err := wsStore.PendingCount()
	if err != nil {
		return "", err
	}
	if pending >= s.workshopCfg().MaxPending {
		return "", fmt.Errorf("workshop has %d pending proposals (max %d)", pending, s.workshopCfg().MaxPending)
	}
	if name == "" {
		name = workshop.SuggestSkillName(focus, body)
	}
	desc := "Background review skill proposal."
	if focus != "" {
		desc = "Focus: " + focus
	}
	fullBody := workshop.DraftSkillBody(name, desc, body)
	proposal, err := wsStore.Create(name, fullBody, source, sessionID, focus, true)
	if err != nil {
		return "", err
	}
	auto := s.skillsAutoApply(ctx, source)
	if auto {
		if err := proposal.Apply(skillsDir); err != nil {
			return fmt.Sprintf("proposal %s created (auto-apply failed: %v)", proposal.Meta.ID, err), nil
		}
		return fmt.Sprintf("skill %q applied from proposal %s", name, proposal.Meta.ID), nil
	}
	return fmt.Sprintf("skill proposal %s created for %q (pending apply)", proposal.Meta.ID, name), nil
}

// NewLearnCapture registers tool/learn-capture for background review and /learn tooling.
func NewLearnCapture(_ struct{}, deps struct {
	Learning *Service `json:"learning"`
}) (agentkit.Tool, error) {
	if deps.Learning == nil {
		return nil, fmt.Errorf("tool/learn-capture requires learning")
	}
	svc := deps.Learning
	return agentkit.NewTool[rtlearning.CaptureInput, rtlearning.CaptureOutput]("learn_capture", func(ctx context.Context, input rtlearning.CaptureInput) (rtlearning.CaptureOutput, error) {
		sid := string(session.SessionIDFromContext(ctx))
		out, err := rtlearning.ApplyCapture(ctx, svc, sid, input)
		if err != nil {
			return rtlearning.CaptureOutput{}, err
		}
		return out, nil
	}).Description("Persist memory or propose a skill during background review (not for general use).").Build()
}

func shouldRunReview(messages []agentkit.ModelMessage, skipSlashOnly bool) bool {
	if len(messages) == 0 {
		return false
	}
	// Require at least one non-command user line in the recent tail.
	limit := 12
	start := 0
	if len(messages) > limit {
		start = len(messages) - limit
	}
	userLines := 0
	for _, msg := range messages[start:] {
		if msg.Role != "user" {
			continue
		}
		text := strings.TrimSpace(flattenParts(msg.Content))
		if text == "" {
			continue
		}
		if skipSlashOnly && strings.HasPrefix(text, "/") {
			continue
		}
		userLines++
	}
	return userLines > 0
}

func flattenParts(parts []agentkit.ContentPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type != "text" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strings.TrimSpace(p.Text))
	}
	return b.String()
}
