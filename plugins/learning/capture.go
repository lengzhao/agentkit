package learning

import (
	"context"
	"fmt"
	"strings"

	"github.com/lengzhao/agentkit"
	caplearning "github.com/lengzhao/agentkit/cap/learning"
	capmemory "github.com/lengzhao/agentkit/cap/memory"
	rtlearning "github.com/lengzhao/agentkit/runtime/learning"
	"github.com/lengzhao/agentkit/runtime/rctx"
	"github.com/lengzhao/agentkit/runtime/session"
)

// CaptureSkillPropose implements caplearning.SkillProposer.
func (s *Service) CaptureSkillPropose(ctx context.Context, name, body, sessionID, focus, source string) (string, error) {
	if !s.skillsWorkshopEnabled(ctx) {
		return "", fmt.Errorf("skill workshop is disabled (use /learn policy skills propose|auto)")
	}
	return s.createSkillProposal(ctx, skillProposeParams{
		Name:       name,
		Body:       body,
		Source:     source,
		SessionID:  sessionID,
		Focus:      focus,
		Autonomous: true,
	})
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
		sid := string(rctx.SessionIDFromContext(ctx))
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
