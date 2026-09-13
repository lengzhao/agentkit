package learning

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/lengzhao/agentkit"
	caplearning "github.com/lengzhao/agentkit/cap/learning"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtlearning "github.com/lengzhao/agentkit/runtime/learning"
	"github.com/lengzhao/agentkit/plugins/learning/workshop"
	"github.com/lengzhao/agentkit/runtime/session"
)

// CaptureSkillPropose implements caplearning.SkillProposer.
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
		return "", fmt.Errorf("workshop has %d pending proposals (max %d); apply or reject first",
			pending, s.workshopCfg().MaxPending)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = workshop.SuggestSkillName(focus, body)
	}
	fullBody := strings.TrimSpace(body)
	if fullBody == "" {
		return "", fmt.Errorf("skill body is empty")
	}
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

// NewLearnCaptureTool builds the isolated review-fork tool (not registered as a separate plugin kind).
func NewLearnCaptureTool(mem capmemory.Capture, skills caplearning.SkillProposer) (agentkit.Tool, error) {
	if mem == nil {
		return nil, fmt.Errorf("learn_capture requires memory")
	}
	if skills == nil {
		return nil, fmt.Errorf("learn_capture requires learning skill proposer")
	}
	return agentkit.NewTool[caplearning.CaptureInput, caplearning.CaptureOutput]("learn_capture", func(ctx context.Context, input caplearning.CaptureInput) (caplearning.CaptureOutput, error) {
		sid := string(session.SessionIDFromContext(ctx))
		out, err := rtlearning.ApplyCapture(ctx, mem, skills, sid, input)
		if err != nil {
			return caplearning.CaptureOutput{}, err
		}
		return out, nil
	}).Description("Persist memory or propose a skill during background review (not for general use).").Build()
}

func shouldRunReview(messages []agentkit.ModelMessage, skipSlashOnly bool) bool {
	if len(messages) == 0 {
		return false
	}
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
		text := strings.TrimSpace(session.FlattenTextParts(msg.Content, " "))
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
